r"""pdfmasker — mask sensitive text in PDFs, in-process.

Basic usage::

    from pdfmasker import mask_pdf

    result = mask_pdf(pdf_bytes, patterns=["Jane Doe", "123-45-6789"])
    masked_pdf = result.document_content   # bytes
    result.entries                         # per-target log: target, replacement, kind, count

Masking is built from four pluggable roles — a backend (how the PDF is read and rewritten), optional detectors
(what to mask), an editor (how each match is rendered), and a substitution store (consistent, collision-free
replacements)::

    from pdfmasker import Masker, RegexDetector, PseudonymizeEditor, InMemorySubstitutionStore

    store = InMemorySubstitutionStore()
    masker = Masker(
        detectors=[RegexDetector(r"\\d{3}-\\d{2}-\\d{4}", kind="ssn")],
        editor=PseudonymizeEditor(store),
    )
    result = masker.mask(pdf_bytes, patterns=["Jane Doe"])
"""

from pdfmasker.backends.pikepdf import PikepdfBackend
from pdfmasker.base import (
    Backend,
    Detection,
    Detector,
    Document,
    Editor,
    MaskResult,
    PlanEntry,
    RenderMode,
    Span,
    Substitution,
    SubstitutionStore,
    TargetEntry,
    TextView,
)
from pdfmasker.detectors import LiteralDetector, RegexDetector
from pdfmasker.editors import FixedCharEditor, FixedStringEditor, KeyedPseudonymizeEditorEditor, PseudonymizeEditor
from pdfmasker.errors import (
    MaskerBackendError,
    MaskerDetectorError,
    MaskerEditorError,
    MaskError,
    MissingDependencyError,
    PdfMaskerError,
)
from pdfmasker.masker import Masker, mask_pdf
from pdfmasker.stores import InMemorySubstitutionStore, LockedSubstitutionStore

__all__ = [
    "Backend",
    "Detection",
    "Detector",
    "Document",
    "Editor",
    "FixedCharEditor",
    "FixedStringEditor",
    "InMemorySubstitutionStore",
    "KeyedPseudonymizeEditorEditor",
    "LiteralDetector",
    "LockedSubstitutionStore",
    "MaskError",
    "MaskResult",
    "Masker",
    "MaskerBackendError",
    "MaskerDetectorError",
    "MaskerEditorError",
    "MissingDependencyError",
    "PdfMaskerError",
    "PikepdfBackend",
    "PlanEntry",
    "PseudonymizeEditor",
    "RegexDetector",
    "RenderMode",
    "Span",
    "Substitution",
    "SubstitutionStore",
    "TargetEntry",
    "TextView",
    "mask_pdf",
]

__version__ = "0.1.0"
