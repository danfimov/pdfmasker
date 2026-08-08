package masker_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/danfimov/pdfmasker/internal/masker"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "test_paystub.pdf"))
	require.NoError(t, err)
	return data
}

func mask(t *testing.T, data []byte, targets ...string) masker.StreamMaskResult {
	t.Helper()
	res, err := masker.MaskStream(masker.StreamMaskRequest{
		Source:   bytes.NewReader(data),
		Targets:  targets,
		MaskWith: "[MASKED]",
	})
	require.NoError(t, err)
	return res
}

func drain(t *testing.T, res masker.StreamMaskResult) []byte {
	t.Helper()
	_, err := res.Reader.Seek(0, io.SeekStart)
	require.NoError(t, err)
	b, err := io.ReadAll(res.Reader)
	require.NoError(t, err)
	return b
}

// Baseline single target still redacts and is idempotent.
func TestChar_SingleTarget(t *testing.T) {
	data := fixture(t)
	res := mask(t, data, "Lorraine Freddie")
	require.Positive(t, res.Applied["Lorraine Freddie"], "expected the full name to be found")

	out := drain(t, res)
	require.Greater(t, len(out), 0)

	// Second pass finds nothing (already masked).
	res2 := mask(t, out, "Lorraine Freddie")
	require.Zero(t, res2.Applied["Lorraine Freddie"])
}

// Multiple targets in one request each get their own count and are all redacted.
func TestChar_MultiTarget(t *testing.T) {
	data := fixture(t)
	res := mask(t, data, "Lorraine", "Freddie")

	require.Positive(t, res.Applied["Lorraine"], "first name must be found")
	require.Positive(t, res.Applied["Freddie"], "last name must be found")

	// Re-masking the output must find neither.
	out := drain(t, res)
	res2 := mask(t, out, "Lorraine", "Freddie")
	require.Zero(t, res2.Applied["Lorraine"])
	require.Zero(t, res2.Applied["Freddie"])
}

// True case-insensitive matching: lowercase and uppercase multi-word queries both
// redact the mixed-case text. (The previous variation scheme missed both — it only
// matched the exact casing for multi-word text.)
func TestChar_CaseInsensitive_Lower(t *testing.T) {
	data := fixture(t)
	res := mask(t, data, "lorraine freddie")
	require.Positive(t, res.Applied["lorraine freddie"], "lowercase query must match mixed-case text")
}

func TestChar_CaseInsensitive_UpperMultiword(t *testing.T) {
	data := fixture(t)
	res := mask(t, data, "LORRAINE FREDDIE")
	require.Positive(t, res.Applied["LORRAINE FREDDIE"], "uppercase query must match mixed-case text")
}

// Output stays structurally valid: it can be re-processed without error.
func TestChar_OutputReusable(t *testing.T) {
	data := fixture(t)
	out := drain(t, mask(t, data, "Lorraine Freddie"))

	_, err := masker.MaskStream(masker.StreamMaskRequest{
		Source:  bytes.NewReader(out),
		Targets: []string{"anything"},
	})
	require.NoError(t, err, "masked PDF must remain parseable")
}

// Hybrid path (object-stream PDF, real ADP paystub): name parts are redacted and
// the operation is idempotent. Guards the ctx-reuse dedup refactor.
func TestChar_HybridADP(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "test_paystub_adp_hybrid.pdf"))
	require.NoError(t, err)

	res := mask(t, data, "ANTWANE", "JEFFERSON-TOLBERT")
	require.Positive(t, res.Applied["ANTWANE"], "first name must be found in hybrid PDF")
	require.Positive(t, res.Applied["JEFFERSON-TOLBERT"], "last name must be found in hybrid PDF")

	out := drain(t, res)
	require.Greater(t, len(out), 0)

	res2 := mask(t, out, "ANTWANE", "JEFFERSON-TOLBERT")
	require.Zero(t, res2.Applied["ANTWANE"], "already masked")
	require.Zero(t, res2.Applied["JEFFERSON-TOLBERT"], "already masked")
}

// A target that does not occur yields zero and does not corrupt output.
func TestChar_NoMatch(t *testing.T) {
	data := fixture(t)
	res := mask(t, data, "Zzzxyq Nonexistent")
	require.Zero(t, res.Applied["Zzzxyq Nonexistent"])
	require.Greater(t, len(drain(t, res)), 0)
}
