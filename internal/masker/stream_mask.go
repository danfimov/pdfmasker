package masker

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// DefaultMaskChar is used to build the mask when no explicit replacement value is provided.
// X is chosen because it's available in almost all PDF fonts.
const DefaultMaskChar = "X"

// StreamMaskRequest describes how input PDF data should be masked.
type StreamMaskRequest struct {
	Source       io.Reader
	MaskWith     string
	Targets      []string
	StopOnErrors bool
}

// StreamMaskResult encapsulates the masked PDF along with per-target statistics.
type StreamMaskResult struct {
	Reader  io.ReadSeeker
	Applied map[string]int
}

// MaskStream applies text masking to a PDF loaded from an arbitrary io.Reader and returns the updated PDF as an io.ReadSeeker.
// It automatically detects PDFs with object streams (PDF 1.5+) and uses a hybrid approach to preserve them correctly.
// Case-insensitive: for each target, it searches for lowercase, UPPERCASE, and Capitalized variants.
func MaskStream(req StreamMaskRequest) (StreamMaskResult, error) {
	var result StreamMaskResult

	if err := req.validate(); err != nil {
		return result, err
	}

	data, err := io.ReadAll(req.Source)
	if err != nil {
		return result, fmt.Errorf("read source: %w", err)
	}

	// Trim and de-duplicate targets. Case variations are no longer generated here:
	// the fallback path matches case-insensitively and the hybrid path expands
	// variations internally, so a single occurrence of each target is enough.
	seen := make(map[string]struct{}, len(req.Targets))
	targets := make([]string, 0, len(req.Targets))
	for _, target := range req.Targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}

	// Use the hybrid approach with automatic fallback. Both paths return counts
	// keyed by the original targets, so no further aggregation is required.
	reader, applied, err := MaskStreamWithFallback(data, targets, req.MaskWith, req.StopOnErrors)
	if err != nil {
		return StreamMaskResult{}, err
	}

	result.Reader = reader
	result.Applied = applied
	return result, nil
}

func (req StreamMaskRequest) validate() error {
	if req.Source == nil {
		return errors.New("source reader is required")
	}
	for _, target := range req.Targets {
		if strings.TrimSpace(target) != "" {
			return nil
		}
	}
	return errors.New("at least one non-empty target is required")
}
