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

func TestMaskStream(t *testing.T) {
	t.Parallel()

	sourcePath := filepath.Join("..", "..", "tests", "fixtures", "paystubs", "simple_paystub.pdf")
	f, err := os.Open(sourcePath)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = f.Close()
	})

	result, err := masker.MaskStream(masker.StreamMaskRequest{
		Source:   f,
		Targets:  []string{"Lorraine Freddie"},
		MaskWith: "[MASKED]",
	})
	require.NoError(t, err)

	count, ok := result.Applied["Lorraine Freddie"]
	require.True(t, ok)
	require.NotZero(t, count)

	_, err = result.Reader.Seek(0, io.SeekStart)
	require.NoError(t, err)

	maskedBytes, err := io.ReadAll(result.Reader)
	require.NoError(t, err)
	require.Greater(t, len(maskedBytes), 0)

	second, err := masker.MaskStream(masker.StreamMaskRequest{
		Source:   bytes.NewReader(maskedBytes),
		Targets:  []string{"Lorraine Freddie"},
		MaskWith: "[MASKED]",
	})
	require.NoError(t, err)

	secondCount, ok := second.Applied["Lorraine Freddie"]
	require.True(t, ok)
	require.Zero(t, secondCount)
}
