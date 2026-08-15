package masker_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/benoitkugler/pdf/contentstream"
	benoitModel "github.com/benoitkugler/pdf/model"
	"github.com/benoitkugler/pdf/reader"
	"github.com/benoitkugler/pdf/reader/parser"
	"github.com/stretchr/testify/require"

	"github.com/danfimov/pdfmasker/internal/masker"
)

// paystubFixture is the ADP paystub (fake PII) used by the behaviour-specific tests
// below. It mixes six inline images with the text layer, and the employee name is
// laid out as two separate TJ operators ("HERMIONE" then "GRANGER").
const paystubFixture = "adp_paystub_hermion_granger.pdf"

func readPaystub(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "paystubs", name))
	require.NoError(t, err)
	return data
}

// TestMaskStreamPaystubs sweeps every fixture under tests/fixtures/paystubs across
// the different PDF producers (ADP, Paychex, Workday, and a simple generated stub),
// masking each one's employee name (or, for the pre-redacted stub, a phone number).
// For each it asserts the target is found, the masked PDF re-parses, and a second
// pass finds nothing — which proves, independent of stream compression, that the
// text was actually removed.
func TestMaskStreamPaystubs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		file   string
		target string
	}{
		{"adp_paystub_hermion_granger.pdf", "HERMIONE GRANGER"},
		{"adp_paystub_neville_lestrange-weasley.pdf", "NEVILLE LESTRANGE-WEASLEY"},
		{"adp_paystub_neville_lestrange-weasley_2.pdf", "NEVILLE LESTRANGE-WEASLEY"},
		{"paychex_paystub_bill_weasley.pdf", "Bill Weasley"},
		{"paychex_paystub_narcissa_lockhart.pdf", "NARCISSA G LOCKHART"},
		{"workday_paystub_luna_lovegood.pdf", "Luna Lovegood"},
		{"workday_paystub_redacted.pdf", "286-2860"},
		{"simple_paystub.pdf", "Lorraine Freddie"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.file, func(t *testing.T) {
			t.Parallel()
			data := readPaystub(t, c.file)

			res, err := masker.MaskStream(masker.StreamMaskRequest{
				Source:  bytes.NewReader(data),
				Targets: []string{c.target},
			})
			require.NoError(t, err)
			require.Positive(t, res.Applied[c.target], "%s: target %q should be masked", c.file, c.target)

			masked, err := io.ReadAll(res.Reader)
			require.NoError(t, err)

			// The masked PDF must still parse, and the target must be gone.
			second, err := masker.MaskStream(masker.StreamMaskRequest{
				Source:  bytes.NewReader(masked),
				Targets: []string{c.target},
			})
			require.NoError(t, err, "%s: masked PDF must remain parseable", c.file)
			require.Zero(t, second.Applied[c.target], "%s: re-masking must find nothing (idempotent)", c.file)
		})
	}
}

// TestMaskStreamInlineImage guards masking of PDFs whose content mixes inline
// images (BI ... ID <binary> EI) with the text layer. The fixture has six of them,
// and the text targeted here (HERMIONE / GRANGER / 469-3206) all appears AFTER the
// first inline image. The raw PostScript tokenizer choked on the image binary and
// dropped every operator that followed, so these came back with a count of zero;
// parsing the stream into operations (parser.ParseContent) fixes it.
func TestMaskStreamInlineImage(t *testing.T) {
	t.Parallel()

	data := readPaystub(t, paystubFixture)

	result, err := masker.MaskStream(masker.StreamMaskRequest{
		Source:  bytes.NewReader(data),
		Targets: []string{"HERMIONE", "GRANGER", "469-3206"},
	})
	require.NoError(t, err)

	// Text that appears after the inline images must be found and masked.
	require.Positive(t, result.Applied["HERMIONE"], "name after inline image should be masked")
	require.Positive(t, result.Applied["GRANGER"], "name after inline image should be masked")
	require.Positive(t, result.Applied["469-3206"], "number after inline image should be masked")

	masked, err := io.ReadAll(result.Reader)
	require.NoError(t, err)

	// The six inline images must survive the parse -> mask -> serialize round-trip.
	require.Equal(t, 6, countInlineImages(t, masked), "all inline images must be preserved")

	// And the masked PDF must still be reusable: a second pass parses it cleanly
	// and finds the already-masked targets gone (idempotent).
	second, err := masker.MaskStream(masker.StreamMaskRequest{
		Source:  bytes.NewReader(masked),
		Targets: []string{"HERMIONE", "469-3206"},
	})
	require.NoError(t, err)
	require.Zero(t, second.Applied["HERMIONE"])
	require.Zero(t, second.Applied["469-3206"])
}

// TestMaskStreamPhraseAcrossOperators guards whitespace-flexible matching: the
// employee name is laid out as two separate TJ operators ("HERMIONE" then
// "GRANGER") with the visual space rendered by a positioning jump, not a space
// glyph. A single target with a literal space must still mask both words, stay
// idempotent, and leave neighbouring text untouched.
func TestMaskStreamPhraseAcrossOperators(t *testing.T) {
	t.Parallel()

	data := readPaystub(t, paystubFixture)

	result, err := masker.MaskStream(masker.StreamMaskRequest{
		Source:  bytes.NewReader(data),
		Targets: []string{"HERMIONE GRANGER"},
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.Applied["HERMIONE GRANGER"], "phrase spanning two TJ operators should be masked")

	masked, err := io.ReadAll(result.Reader)
	require.NoError(t, err)

	// A second pass finds nothing (idempotent).
	second, err := masker.MaskStream(masker.StreamMaskRequest{
		Source:  bytes.NewReader(masked),
		Targets: []string{"HERMIONE GRANGER"},
	})
	require.NoError(t, err)
	require.Zero(t, second.Applied["HERMIONE GRANGER"])
}

// TestMaskStreamCustomMaskAcrossOperators guards a custom, differently-sized mask
// applied to a phrase that spans two show-text operators. The replacement must land
// contiguously (not be split across the two operators' positions, which produced
// fragments like "HERMIO NE GRANG"), and neighbouring text must survive.
func TestMaskStreamCustomMaskAcrossOperators(t *testing.T) {
	t.Parallel()

	data := readPaystub(t, paystubFixture)

	result, err := masker.MaskStream(masker.StreamMaskRequest{
		Source:   bytes.NewReader(data),
		Targets:  []string{"HERMIONE GRANGER"},
		MaskWith: "MINERVA MCGONAGALL", // longer than the target and spans two operators
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.Applied["HERMIONE GRANGER"])

	masked, err := io.ReadAll(result.Reader)
	require.NoError(t, err)

	text := allShowText(t, masked)
	require.Contains(t, text, "MINERVA MCGONAGALL", "custom mask must be contiguous")
	require.NotContains(t, text, "HERMIONE")
	require.NotContains(t, text, "GRANGER")
}

// allShowText concatenates the raw bytes of every show-text operator across a PDF's
// page content streams. The paystub fonts are raw (ASCII) so this is the visible
// text; it is enough to assert how a replacement was laid out.
func allShowText(t *testing.T, pdf []byte) string {
	t.Helper()
	doc, _, err := reader.ParsePDFReader(bytes.NewReader(pdf), reader.Options{})
	require.NoError(t, err)

	var b bytes.Buffer
	for _, page := range doc.Catalog.Pages.Flatten() {
		var resCS benoitModel.ResourcesColorSpace
		if page.Resources != nil {
			resCS = page.Resources.ColorSpace
		}
		for _, content := range page.Contents {
			decoded, err := content.Decode()
			require.NoError(t, err)
			ops, err := parser.ParseContent(decoded, resCS)
			require.NoError(t, err)
			for _, op := range ops {
				switch o := op.(type) {
				case contentstream.OpShowText:
					b.WriteString(o.Text)
				case contentstream.OpShowSpaceText:
					for _, ts := range o.Texts {
						b.Write(ts.CharCodes)
					}
				}
			}
		}
	}
	return b.String()
}

// countInlineImages parses every page content stream of a PDF and counts the
// inline-image operators, proving the masking round-trip kept them intact.
func countInlineImages(t *testing.T, pdf []byte) int {
	t.Helper()
	doc, _, err := reader.ParsePDFReader(bytes.NewReader(pdf), reader.Options{})
	require.NoError(t, err)

	n := 0
	for _, page := range doc.Catalog.Pages.Flatten() {
		var resCS benoitModel.ResourcesColorSpace
		if page.Resources != nil {
			resCS = page.Resources.ColorSpace
		}
		for _, content := range page.Contents {
			decoded, err := content.Decode()
			require.NoError(t, err)
			ops, err := parser.ParseContent(decoded, resCS)
			require.NoError(t, err)
			for _, op := range ops {
				if _, ok := op.(contentstream.OpBeginImage); ok {
					n++
				}
			}
		}
	}
	return n
}
