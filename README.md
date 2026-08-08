# pdfmasker

Mask sensitive text in PDFs.

## Usage

```python
from pdfmasker import mask_pdf

pdf_bytes = open("paystub.pdf", "rb").read()

result = mask_pdf(pdf_bytes, patterns=["Jane Doe", "123-45-6789"])

result.pdf       # bytes — the masked PDF
result.counts    # {"Jane Doe": 2, "123-45-6789": 1}
```

`mask_with` controls the replacement string; omit it (or pass `None`) to use the default mask (a run of `X` matching
each target's length):

```python
mask_pdf(pdf_bytes, patterns=["Jane Doe"], mask_with="[REDACTED]")
```

Errors raise `pdfmasker.MaskError`.

## Contributing

The architecture, repository layout, setup, dev commands (`make help`), the build/cross-build model, and the release
flow all live in [`CONTRIBUTING.md`](CONTRIBUTING.md).
