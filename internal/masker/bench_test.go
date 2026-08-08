package masker_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/danfimov/pdfmasker/internal/masker"
)

func loadFixture(b *testing.B) []byte {
	b.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "test_paystub.pdf"))
	if err != nil {
		b.Fatalf("read fixture: %v", err)
	}
	return data
}

func benchMask(b *testing.B, targets []string) {
	data := loadFixture(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := masker.MaskStream(masker.StreamMaskRequest{
			Source:   bytes.NewReader(data),
			Targets:  targets,
			MaskWith: "[MASKED]",
		})
		if err != nil {
			b.Fatalf("mask: %v", err)
		}
		if _, err := io.Copy(io.Discard, res.Reader); err != nil {
			b.Fatalf("drain: %v", err)
		}
	}
}

// Single target — the common admin case masks a single full name.
func BenchmarkMaskStream_SingleTarget(b *testing.B) {
	benchMask(b, []string{"Lorraine Freddie"})
}

// Several targets — each expands to up to 4 case variations, so this exercises
// the multi-pass scanning behaviour.
func BenchmarkMaskStream_MultiTarget(b *testing.B) {
	benchMask(b, []string{"Lorraine Freddie", "Lorraine", "Freddie", "123-45-6789", "Acme Corp"})
}

func benchMaskFile(b *testing.B, name string, targets []string) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", name))
	if err != nil {
		b.Fatalf("read fixture: %v", err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := masker.MaskStream(masker.StreamMaskRequest{
			Source:   bytes.NewReader(data),
			Targets:  targets,
			MaskWith: "[MASKED]",
		})
		if err != nil {
			b.Fatalf("mask: %v", err)
		}
		if _, err := io.Copy(io.Discard, res.Reader); err != nil {
			b.Fatalf("drain: %v", err)
		}
	}
}

// Real ADP paystub — routes through the hybrid (object-stream) path, which is the
// production flow for ADP/Workday. Patterns mirror the admin masking values.
func BenchmarkMaskStream_HybridADP(b *testing.B) {
	benchMaskFile(b, "test_paystub_adp_hybrid.pdf", []string{
		"ANTWANE", "JEFFERSON-TOLBERT", "AMYX INC", "6605 SPRINGFORD TERRACE",
	})
}
