# pdfmasker

[![PyPI - Python Version](https://img.shields.io/pypi/pyversions/pdfmasker?style=for-the-badge)](https://pypi.org/project/pdfmasker/)
[![PyPI](https://img.shields.io/pypi/v/pdfmasker?style=for-the-badge)](https://pypi.org/project/pdfmasker/)
[![PyPI - Downloads](https://img.shields.io/pypi/dm/pdfmasker?style=for-the-badge)](https://pypistats.org/packages/pdfmasker)
[![CodSpeed Badge](https://img.shields.io/endpoint?url=https://codspeed.io/badge.json&style=for-the-badge)](https://codspeed.io/danfimov/pdfmasker?utm_source=badge)

With this library you can mask sensitive text in PDFs or basically change any text in your PDF files.

## Usage

```python
from pdfmasker import mask_pdf

pdf_bytes = open("paystub.pdf", "rb").read()

result = mask_pdf(pdf_bytes, patterns=["Jane Doe", "123-45-6789"])

result.document_content   # bytes — the masked PDF
result.entries            # per-target log: target, replacement, kind, count
```

By default each match is replaced in place with a run of `X`. An `editor` chooses how matches are rendered — a
constant string, or consistent pseudonyms:

```python
from pdfmasker import FixedStringEditor, mask_pdf

mask_pdf(pdf_bytes, patterns=["Jane Doe"], editor=FixedStringEditor("[REDACTED]"))
```

## Pluggable parts

Masking is composed from four pluggable parts:

- **Backend** — how the PDF is read and rewritten. The default in-process `PikepdfBackend` edits content streams
  directly in Python.
- **Detector** (optional) — what to mask. Pass literal `patterns`, or configure detectors such as `RegexDetector`
  to discover targets from the document text.
- **Editor** — how each match is rendered: `FixedCharEditor` (default `X`), `FixedStringEditor`, `PseudonymizeEditor`,
  or `KeyedPseudonymizeEditor`.
- **SubstitutionStore** — keeps pseudonyms consistent and collision-free. Share one across documents so the same
  value always masks to the same replacement.

```python
from pdfmasker import InMemorySubstitutionStore, Masker, PseudonymizeEditor, RegexDetector

store = InMemorySubstitutionStore()
masker = Masker(
    detectors=[RegexDetector(r"\d{3}-\d{2}-\d{4}", kind="ssn")],
    editor=PseudonymizeEditor(store),
)
result = masker.mask(pdf_bytes, patterns=["Jane Doe"])
```

## Contributing

The architecture, repository layout, setup, dev commands (`make help`), the build/cross-build model, and the release
flow all live in [`CONTRIBUTING.md`](CONTRIBUTING.md).
