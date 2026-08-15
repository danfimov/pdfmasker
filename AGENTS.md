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

### Alternative in-process backend (pikepdf)

`PikepdfTextLayerStrategy` (`strategies/pikepdf/`) is a second, independent implementation that does the same masking
entirely in Python via pikepdf/qpdf — no subprocess, no Go. It is **opt-in**: the default `Masker` still uses
`TextLayerStrategy`, and callers select pikepdf explicitly with
`Masker(strategies=[PikepdfTextLayerStrategy()])`. pikepdf is an optional dependency (`pdfmasker[pikepdf]`); importing
the subpackage without it raises `MissingDependencyError`. qpdf round-trips object streams natively, so this backend
needs no structure-based engine split. It reconstructs the same per-font segments and does the same
whitespace-flexible matching (a space in a target matches a real space glyph or an operator boundary), so a full name
split across `Tj`/`TJ` operators masks identically to the Go path. The two backends share no code and are kept in sync
only through the tests in `tests/` (Go path) and `tests/pikepdf/` (Python path) — the fixtures in
`tests/fixtures/paystubs/` carry a `<name>.keys.json` sidecar of sensitive strings that both benchmark suites mask.

### The two Go masking engines (the crux of this codebase)

`MaskStreamWithFallback` parses the PDF once with pdfcpu, then branches on `hasObjectStreams(ctx)`:

- **Hybrid path** (`MaskStreamHybrid`, `hybrid_writer.go`) — PDF 1.5+ with object streams (real ADP/Workday paystubs).
  Uses pdfcpu to extract font ToUnicode CMaps but `benoitkugler/pdf` to parse and *rewrite* the document, because pdfcpu
  does not round-trip object/xref streams safely.
- **Fallback path** (`replaceTextInContext`, `text_replace.go`) — simpler PDFs, edited in place with pdfcpu.

Both engines share the **same content-stream masking core** (`maskOperations`, `content_ops.go`), match
**case-insensitively** (`runeEqualFold` in `fold.go`, no case-variation expansion), and key their result counts by the
original target string, so the caller sees one uniform `applied` map. The engines differ only in how they read/write the
*document* (benoitkugler vs pdfcpu) and how they collect fonts.

Matching is also **whitespace-flexible** (`matchFlexAt`): a space in a target matches either real whitespace or a
boundary between two show-text operators (zero width). This is what lets a full-name target like `"HERMIONE GRANGER"`
match text where the visual space between the words is a positioning jump between separate `TJ` operators rather than a
space glyph. It deliberately does *not* match inside a single word (no boundary, no whitespace), so `"in come"` never
matches `income`. Boundaries are per show-text operator, not per `TJ` array element, so intra-word kerning splits aren't
treated as spaces.

### Content-stream parsing (`benoitkugler/pdf/reader/parser.ParseContent`)

Content streams are parsed into `[]contentstream.Operation` with `parser.ParseContent`, not the raw PostScript
tokenizer. This is the crux of correct inline-image handling: `ParseContent` understands `BI ... ID <binary> EI` and
skips the image's binary payload by its computed length, whereas the bare tokenizer (`pstokenizer`) deliberately halts at
`ID`/`stream` (it can't know where binary ends), which silently dropped every operator — and thus all text — after the
first inline image. `maskOperations` edits the ops and `contentstream.WriteOperations` serializes them back, inline
images and all.

This depends on a **local fork** of `benoitkugler/pdf` wired via a `replace` directive in `go.mod` (see "Fork
dependency" below): upstream `v0.0.15` errors on `/IM true` inline image masks (no color space) and adds a stray byte
around inline data that breaks the parse→serialize→parse round-trip.

### Content-stream editing invariants

When touching `maskOperations` / `content_ops.go`, preserve these or you will corrupt PDFs:

- **CID vs simple fonts.** CID (Type0) fonts encode text as 2-byte slots via a ToUnicode CMap; simple fonts are 1 byte.
  Decoding and re-encoding branch on `isCID` in `glyphFont`. A font with no ToUnicode is treated as raw bytes (UTF-16 BOM
  aware). Unmapped CIDs / chunks that decode to nothing are intentionally left untouched.
- **Byte-slot preservation.** Replacements re-encode to the *same byte count* as the original operand
  (`encodeWithMappingSlots`), padding/truncating with a fallback glyph, so the visual layout and downstream offsets stay
  stable. `TJ` kerning adjustments are preserved because the parser keeps them as `TextSpaced.SpaceSubtractedAfter`.
- **Two masking modes, handled per chunk when writing back** (`replacement.maskChar`):
  - *Default mask* (no `mask_with`): each matched rune is replaced in place by `X` (`DefaultMaskChar`), so every chunk
    keeps its rune length and CID byte slots — layout is preserved and `encode` re-encodes with `preserveSlots=true`.
  - *Custom mask* (`mask_with` set): the whole matched span is emitted as one unit into the chunk holding the match's
    *start*, and the other chunks the match spans drop their matched text. This keeps a differently-sized, multi-word
    replacement contiguous instead of being split across the original operators' positions — masking the two-operator
    name `"HERMIONE GRANGER"` with `"MINERVA MCGONAGALL"`, the earlier char-for-char redistribution fragmented the
    replacement across the two positions. When a chunk's length changes, `encode` uses `preserveSlots=false` so the text
    is encoded at its natural length rather than truncated to the original byte count.
- Text may be split character-by-character across many `Tj`/`TJ` operators; `maskOperations` reconstructs a *segment*
  (a run of consecutive show-text ops under one font, bounded by font changes and `BT`/`ET`), matches against the full
  reconstructed text, then redistributes the replacement back, re-encoding only the chunks whose characters changed.

## Fork dependency (`benoitkugler/pdf`)

`go.mod` requires `github.com/benoitkugler/pdf v0.0.15` but redirects it to a pinned fork commit:

```
replace github.com/benoitkugler/pdf => github.com/danfimov/pdf v0.0.0-20260809121144-9b479295d869
```

(fork repo: https://github.com/danfimov/pdf). The fork carries two fixes needed for inline-image content-stream
parsing, both in the inline-image path:

1. `contentstream.OpBeginImage.Metrics` returns `(1, 1, nil)` for `ImageMask` stencils instead of trying to resolve a
   (nonexistent) color space — otherwise `ParseContent` errors with `missing color space` on `BI /IM true ... ID ... EI`.
2. `reader/parser.parseImageData` reads the inline data as *exactly* the image bytes (skip the single separator space,
   then `SkipBytes(n)`), instead of folding the space into `Image.Content`. That makes read symmetric with
   `OpBeginImage.Add` (`"ID " + Content + "EI"`) so a stream survives parse→serialize→parse.

Both fixes are reported upstream; once merged, bump to the release that includes them and drop the `replace`.

**Rebuild caveat:** `uv run` does *not* recompile the Go binary when only Go sources or `go.mod` change (it reuses the
cached build unless a Python source changes). After editing Go or the fork pin, run `uv run --reinstall pytest` (or set
`PDFMASKER_BINARY` to a freshly `go build`-ed binary) so pytest exercises the new binary rather than a stale one.

## Packaging model

One wheel per OS/arch, tagged `py3-none-<platform>` (Python-agnostic, platform-specific). `hatch_build.py`'s
`CustomBuildHook` compiles `cmd/masker` with `CGO_ENABLED=0` and force-includes the binary at `pdfmasker/_bin/`.
`src/pdfmasker/_bin/` is VCS-ignored, so `pyproject.toml` declares it as a wheel `artifacts` entry. The static binary
needs no libc, so the linux wheel is tagged manylinux2014 with no auditwheel step.

## Test fixtures

All fixtures live in `tests/fixtures/paystubs/` (one directory, shared by both suites). Their PII has been replaced with
fake Harry Potter names of matching length/form so layout is preserved — **fixtures must not contain real PII; scrub
before adding.** The Go tests in `internal/masker` load them via `../../tests/fixtures/paystubs`; the Python suite loads
them through `tests/conftest.py`, which globs `fixtures/paystubs/*.pdf` into `files_for_test` keyed by filename.

The set spans both engines and all three producers, so keep both suites green when touching either engine:

- **Fallback (non-object-stream) path:** `simple_paystub.pdf` (name `Lorraine Freddie` — the general-purpose fallback
  fixture used by `characterization_test.go`, `stream_mask_test.go`, `bench_test.go`), `adp_paystub_neville_lestrange-weasley.pdf`
  (+`_2`), `paychex_paystub_bill_weasley.pdf`, `paychex_paystub_narcissa_lockhart.pdf`.
- **Hybrid (object-stream) path:** `adp_paystub_hermion_granger.pdf` (also the inline-image / cross-operator fixture for
  `inline_image_test.go`; `HERMIONE`/`GRANGER` split across two TJ operators), `workday_paystub_luna_lovegood.pdf`,
  `workday_paystub_redacted.pdf` (name already redacted — masked by phone number).

`internal/masker/inline_image_test.go`'s `TestMaskStreamPaystubs` sweeps every fixture across both engines.
