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

## Masking backends

Text replacement is available through two independent implementations. `mask_pdf` uses the first one by default; the
second is opt-in.

- **Bundled binary (default).** `mask_pdf` and `Masker()` drive a compiled Go engine over a subprocess. It works out of
  the box with the installed wheel and needs no extra dependencies.
- **In-process (pikepdf).** `PikepdfTextLayerStrategy` edits the PDF's content streams directly in Python, avoiding the
  per-call subprocess overhead. It is an optional dependency:

  ```bash
  pip install pdfmasker[pikepdf]
  ```

  ```python
  from pdfmasker import Masker
  from pdfmasker.strategies.pikepdf import PikepdfTextLayerStrategy

  masker = Masker(strategies=[PikepdfTextLayerStrategy()])
  result = masker.mask(pdf_bytes, patterns=["Jane Doe"])
  ```

Both accept the same patterns and `mask_with`, and return the same `MaskResult`. The bundled binary is the more
battle-tested path; the pikepdf backend trades that maturity for lower latency and a pure-Python dependency. Without the
`pikepdf` extra installed, using the in-process strategy raises `pdfmasker.MissingDependencyError`.

## Contributing

The architecture, repository layout, setup, dev commands (`make help`), the build/cross-build model, and the release
flow all live in [`CONTRIBUTING.md`](CONTRIBUTING.md).
