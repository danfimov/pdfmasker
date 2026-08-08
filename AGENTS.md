# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A **hybrid Go + Python** library that masks sensitive text in PDFs in-process. The public surface is a small Python
package (`pdfmasker`); the actual PDF editing is a Go engine (`internal/masker`) compiled to a static binary
(`masker`) that is bundled inside the Python wheel and driven over a subprocess pipe. Changing masking behavior almost
always means editing Go, not Python.

## Commands

```bash
# Python tests (end-to-end, real fixtures). `uv run` reinstalls the package,
# which runs hatch_build.py to (re)compile the Go binary into the install first.
uv run pytest
uv run pytest tests/test_mask.py::test_masks_object_stream_pdf_hybrid_path  # single test

# Go engine tests / benchmarks (fast; no wheel build)
go test ./internal/...
go test ./internal/masker -run TestFold
go test ./internal/masker -bench . -benchmem

# Lint
uv run ruff check src tests

# Build the platform wheel (needs the Go toolchain)
uv build --wheel
GOOS=linux GOARCH=amd64 uv build --wheel   # cross-build, static, no Docker/auditwheel
```

**Fast Go/Python iteration without rebuilding the wheel:** build the binary once with `go build -o /tmp/masker
./cmd/masker`, then set `PDFMASKER_BINARY=/tmp/masker`. `_binary.py` prefers that env var over the bundled
`_bin/masker`, so pytest and the Python API pick up a freshly built Go binary without reinstalling.

## Architecture and control flow

The single request flows through these layers, each in its own file:

1. `mask_pdf()` (`pipeline.py`) → `Masker` routes to the first `MaskStrategy` whose `applies_to()` returns true.
   Strategies are ordered, not competing: text-layer and scanned PDFs are disjoint inputs. Only `TextLayerStrategy` is
   real; `OcrStrategy` is a deliberate, unimplemented extension point placed *ahead* of text in custom pipelines.
2. `TextLayerStrategy` (`strategies/text_layer.py`) shells out to the `masker` binary: **PDF bytes on stdin, masked
   PDF on stdout, a JSON report on stderr** (`{"applied":{...}}` on success, `{"error":...}` + non-zero exit
   on failure). This stdin/stdout/stderr contract is defined in `cmd/masker/main.go` and is the seam between the two
   languages — keep both sides in sync when changing it.
3. `masker.MaskStream` → `MaskStreamWithFallback` (`stream_mask.go`, `hybrid_writer.go`) picks one of **two engines**
   based on the PDF's structure.

### The two Go masking engines (the crux of this codebase)

`MaskStreamWithFallback` parses the PDF once with pdfcpu, then branches on `hasObjectStreams(ctx)`:

- **Hybrid path** (`MaskStreamHybrid`, `hybrid_writer.go`) — PDF 1.5+ with object streams (real ADP/Workday paystubs).
  Uses pdfcpu to extract font ToUnicode CMaps but `benoitkugler/pdf` to parse and *rewrite* the document, because pdfcpu
  does not round-trip object/xref streams safely. Matches strings **exactly**, so targets are first expanded into case
  variations (`generateCaseVariations`) and the per-variation counts are folded back onto the original targets after the
  run.
- **Fallback path** (`replaceTextInContext`, `text_replace.go`) — simpler PDFs, edited in place with pdfcpu. Matches
  **case-insensitively** via the Unicode simple-fold helpers in `fold.go`, so no case expansion is needed here.

Both paths share the same low-level content-stream mechanics and both key their result counts by the original target
string, so the caller sees one uniform `applied` map.

### Content-stream editing invariants

When touching the token-rewriting code (`processTokensHybrid`, `applyReplacement*`, `replaceAcrossTokensInPlace`),
preserve these or you will corrupt PDFs:

- **CID vs simple fonts.** CID (Type0) fonts encode text as 2-byte slots via a ToUnicode CMap; simple fonts are 1 byte.
  Decoding, matching, and re-encoding all branch on `isCIDFont`. Unmapped CIDs are intentionally left untouched.
- **Byte-slot preservation.** Replacements re-encode to the *same slot/byte count* as the original token
  (`encode*WithSlots`), padding/truncating with a fallback glyph, so the visual layout and downstream offsets stay
  stable. The default mask is a run of `X` (`DefaultMaskChar`) sized to the target's rune length.
- **Inline images** (`BI ... ID <binary> EI`) contain binary that breaks the PS tokenizer, so they're extracted to
  placeholders before tokenizing and restored after (`extractInlineImages` / `restoreInlineImages`, hybrid path only).
- Text may be split character-by-character across many `Tj`/`TJ` tokens; `applyReplacementWithReconstruction`
  reassembles a segment's full text to find matches that span tokens, then redistributes the replacement back,
  re-encoding only the tokens whose characters actually changed.

## Packaging model

One wheel per OS/arch, tagged `py3-none-<platform>` (Python-agnostic, platform-specific). `hatch_build.py`'s
`CustomBuildHook` compiles `cmd/masker` with `CGO_ENABLED=0` and force-includes the binary at `pdfmasker/_bin/`.
`src/pdfmasker/_bin/` is VCS-ignored, so `pyproject.toml` declares it as a wheel `artifacts` entry. The static binary
needs no libc, so the linux wheel is tagged manylinux2014 with no auditwheel step.

## Test fixtures

`tests/fixtures/test_paystub.pdf` exercises the fallback path; `tests/fixtures/test_paystub_adp_hybrid.pdf` exercises
the hybrid/object-stream path. They are shared by both suites — the Go tests in `internal/masker` load them via
`../../tests/fixtures`. Any change to either engine should keep both the Go tests and `tests/test_mask.py` green, since
together they are the only coverage of the language seam.
