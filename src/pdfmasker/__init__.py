"""pdfmasker — mask sensitive text in PDFs, in-process.

Basic usage::

    from pdfmasker import mask_pdf

    result = mask_pdf(pdf_bytes, patterns=["Jane Doe", "123-45-6789"])
    masked_pdf = result.pdf          # bytes
    result.counts                     # {"Jane Doe": 2, "123-45-6789": 1}
"""

from pdfmasker.errors import BinaryNotFoundError, MaskError, PdfMaskerError
from pdfmasker.pipeline import Masker, mask_pdf
from pdfmasker.strategies import (
    MaskResult,
    MaskStrategy,
    TextLayerStrategy,
)

__all__ = [
    "BinaryNotFoundError",
    "MaskError",
    "MaskResult",
    "MaskStrategy",
    "Masker",
    "PdfMaskerError",
    "TextLayerStrategy",
    "mask_pdf",
]

__version__ = "0.1.0"
