class PdfMaskerError(Exception):
    """Base class for all pdfmasker errors."""


class MaskError(PdfMaskerError):
    """Masking failed (invalid PDF, invalid patterns, or backend failure)."""


class BinaryNotFoundError(PdfMaskerError):
    """The bundled masking binary could not be located."""


class MissingDependencyError(PdfMaskerError):
    """An optional dependency required by the chosen strategy is not installed."""
