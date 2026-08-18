class PdfMaskerError(Exception):
    """Base class for all pdfmasker errors."""


class MaskError(PdfMaskerError):
    """Masking failed (invalid PDF, invalid patterns, or a role failure).

    The per-role errors below subclass this, so `except MaskError` still catches any failure while callers that care can
    narrow to the layer that broke.
    """


class MaskerBackendError(MaskError):
    """A backend could not parse, select, or write the document."""


class MaskerDetectorError(MaskError):
    """A detector raised while scanning the document text."""


class MaskerEditorError(MaskError):
    """An editor or its substitution store could not produce a replacement."""


class MissingDependencyError(PdfMaskerError):
    """An optional dependency required by the chosen backend is not installed."""
