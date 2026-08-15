"""In-process pikepdf masking strategy (optional; `pip install pdfmasker[pikepdf]`).

Usage::

    from pdfmasker import Masker
    from pdfmasker.strategies.pikepdf import PikepdfTextLayerStrategy

    masker = Masker(strategies=[PikepdfTextLayerStrategy()])
    result = masker.mask(pdf_bytes, patterns=["Jane Doe"])

Importing this package without pikepdf installed raises a clear install hint
rather than a bare `ModuleNotFoundError`.
"""

from pdfmasker.errors import MissingDependencyError

try:
    from pdfmasker.strategies.pikepdf.strategy import PikepdfTextLayerStrategy
except ModuleNotFoundError as exc:
    if exc.name != "pikepdf":
        raise
    message = "pikepdf is not installed. Install it with: pip install pdfmasker[pikepdf]"
    raise MissingDependencyError(message) from exc

__all__ = ["PikepdfTextLayerStrategy"]
