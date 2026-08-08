// Command masker is a thin stdin->stdout wrapper over the internal/masker masking
// core, intended to be invoked as a subprocess from the pdfmasker Python package.
//
// Contract:
//
//	stdin   : the source PDF bytes
//	-patterns : JSON array of strings to mask, e.g. `["Jane Doe","123-45-6789"]` (required)
//	-mask     : replacement string (optional; empty means "repeat the default mask char")
//	stdout  : the masked PDF bytes
//	stderr  : a single JSON object with per-target counts, e.g.
//	          {"applied":{"Jane Doe":2},"log":""}
//	          or, on failure, {"error":"..."} together with a non-zero exit code.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log"
	"os"

	"github.com/danfimov/pdfmasker/internal/masker"
)

type result struct {
	Applied map[string]int `json:"applied,omitempty"`
	Log     string         `json:"log,omitempty"`
	Error   string         `json:"error,omitempty"`
}

func main() {
	var (
		patternsJSON = flag.String("patterns", "", "JSON array of strings to mask (required)")
		maskWith     = flag.String("mask", "", "replacement string (empty = default mask)")
	)
	flag.Parse()

	// Capture any logging the masking core emits so stderr stays pure JSON.
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	log.SetFlags(0)

	var patterns []string
	if err := json.Unmarshal([]byte(*patternsJSON), &patterns); err != nil {
		fail("invalid -patterns (expected JSON array of strings): "+err.Error(), &logBuf)
	}

	res, err := masker.MaskStream(masker.StreamMaskRequest{
		Source:       os.Stdin,
		Targets:      patterns,
		MaskWith:     *maskWith,
		StopOnErrors: false,
	})
	if err != nil {
		fail(err.Error(), &logBuf)
	}

	// Stream the masked PDF to stdout before reporting stats.
	if _, err := io.Copy(os.Stdout, res.Reader); err != nil {
		fail("write masked pdf to stdout: "+err.Error(), &logBuf)
	}

	emit(result{
		Applied: res.Applied,
		Log:     logBuf.String(),
	}, 0)
}

func fail(msg string, logBuf *bytes.Buffer) {
	emit(result{Error: msg, Log: logBuf.String()}, 1)
}

// emit writes the result as JSON to stderr and exits with the given code.
func emit(r result, code int) {
	enc := json.NewEncoder(os.Stderr)
	_ = enc.Encode(r)
	os.Exit(code)
}
