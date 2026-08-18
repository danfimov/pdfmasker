# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A **pure-Python** library that masks sensitive text in PDFs in-process. The public surface is the `pdfmasker` package;
the actual PDF editing runs in Python on top of **pikepdf/qpdf**. Masking is composed from four pluggable roles, so each
axis varies independently.

## Commands

```bash
# Tests (end-to-end, real fixtures)
uv run pytest
uv run pytest tests/pikepdf/test_mask.py::test_masks_object_stream_pdf  # single test

# Lint & types
uv run ruff check src tests
uv run ty check src

# Benchmarks (CodSpeed)
uv run pytest tests/ --codspeed

# Build the wheel (pure Python → one universal py3-none-any wheel)
uv build --wheel
```

## The four roles

Interfaces are structural `typing.Protocol`s in `src/pdfmasker/abc/`, alongside the frozen data types they exchange.
Implementations live in sibling packages:

- **Backend** (`backends/`) — read & rewrite a document. `applies_to(bytes)` + `open(bytes) -> Document`; the
  `Document` handle (`extract_text`, `edit`, `close`) parses once so extraction and editing share it. pikepdf now,
  OCR later.
- **Detector** (`detectors/`) — decide *what* to mask from the text. `LiteralDetector` (the sugar behind `patterns`),
  `RegexDetector`. Optional.
- **Editor** (`editors/`) — decide *how* each match is rendered. `FixedCharEditor`, `FixedStringEditor`, `PseudonymizeEditor`,
  `KeyedPseudonymizeEditorEditor`.
- **SubstitutionStore** (`stores/`) — consistent, collision-free replacements. `InMemorySubstitutionStore`.

## Control flow

`mask_pdf()` / `Masker.mask()` (`masker.py`):

1. Select the first backend whose `applies_to()` is true; `backend.open(data)` → a `Document` (parsed once).
2. Gather `Detection`s: `LiteralDetector(patterns)` always; **only if detectors are configured** does it call
   `document.extract_text()` and run them. **Patterns-only never reads the text → single pass.**
3. `_build_plan` dedups to one `PlanEntry` per distinct target (first detection wins), resolving each via
   `editor.edit()` (where a store-backed editor consults its store).
4. `document.edit(plan)` applies everything; the result carries per-target counts, surfaced as
   `MaskResult.entries` (target, replacement, kind, count).

## The pikepdf backend (the crux)

`backends/pikepdf/` holds the low-level **engine** (`content.py`, `fonts.py`, `fold.py`, `cmap.py`) and the role
**adapter** (`backend.py`). `PikepdfBackend.open` returns a `PikepdfDocument` holding the opened `pikepdf.Pdf`.

### Content-stream editing invariants (`content.py` / `fonts.py`)

Preserve these or you will corrupt PDFs:

- **Segments.** A *segment* is a run of consecutive show-text ops under one font, bounded by font changes (`Tf`) and
  text-block markers (`BT`/`ET`). `iter_segments` yields them once; `process()` masks, `extract_segments()` reads.
  There is **no whole-document text** — matching is per-segment, so a target split across a font change is not
  matchable (and not detectable). Content streams are parsed with `pikepdf.parse_content_stream`, which understands
  `BI ... ID <binary> EI`; non-`ContentStreamInstruction` items (inline images) are skipped but do **not** break a
  segment.
- **Whitespace-flexible, case-insensitive matching** (`fold.match_flex_at`). Comparison is under `casefold()`. A space
  in a target matches either real whitespace **or** a zero-width boundary between two show-text operators — this is what
  lets a full name like `"HERMIONE GRANGER"` match when the visual space is a positioning jump between separate `TJ`
  operators. It never matches inside a single word (no boundary, no whitespace).
- **CID vs simple fonts** (`fonts.py`). Type0 (CID) fonts encode 2-byte slots resolved through a ToUnicode CMap
  (`cmap.parse_tounicode`); simple fonts are 1 byte; with no ToUnicode, bytes pass through as Latin-1. Unmapped CIDs
  decode to nothing and are intentionally left untouched.
- **Byte-slot preservation.** The default per-char mask re-encodes to the **same slot count** (`preserve_slots=True`),
  so glyph positions and downstream offsets stay stable; a length-changing custom replacement encodes at natural length
  (`preserve_slots=False`).
- **Two render modes** (`RenderMode`, mapped to the engine's internal `Replacement` in `backend._to_replacement`):
  - `PER_CHAR` — fill each matched glyph with one char (`FixedCharEditor`, default `X`): length- and slot-preserving,
    idempotent.
  - `WHOLE_SPAN` — emit the whole replacement once at the match start, contiguous (`FixedStringEditor`, pseudonyms): keeps a
    differently sized or multi-word replacement in one piece across the operators that render the match.

## Editors & batch consistency

`PseudonymizeEditor`/`KeyedPseudonymizeEditorEditor` produce stable pseudonyms. `InMemorySubstitutionStore.get_or_assign` is the single
mutation point (the seam a future thread/process-safe store locks) and caps retries, raising `MaskerEditorError`
rather than looping forever. Share one store across a batch for cross-file consistency — this is **sequential** because
the store mutates. For a parallel batch use **process** workers (threads don't help: the masking hot path is GIL-bound
Python) with stateless `KeyedPseudonymizeEditorEditor(key, store=None)`, trading the collision-free guarantee for
order-independence.

## Error model

`PdfMaskerError → MaskError → {MaskerBackendError, MaskerDetectorError, MaskerEditorError}` — the role errors subclass
`MaskError`, so `except MaskError` still catches everything. The backend raises `MaskerBackendError` on open/select/save
failures; the orchestrator wraps the **plugin boundaries** (`_run_detector`, `_render_substitution`), re-raising our own
`PdfMaskerError`s unwrapped and tagging foreign exceptions with which plugin failed.

## Test fixtures

All fixtures live in `tests/fixtures/paystubs/`. Their PII is replaced with fake Harry Potter names of matching
length/form so layout is preserved — **fixtures must not contain real PII; scrub before adding.** Each `<name>.pdf` has
a `<name>.keys.json` sidecar of sensitive strings that the benchmarks mask; `tests/conftest.py` globs them into
`files_for_test`. The engine unit tests (`tests/pikepdf/test_{cmap,fold,fonts}.py`) import `pdfmasker.backends.pikepdf.*`
directly, so those module paths are part of the test contract.
