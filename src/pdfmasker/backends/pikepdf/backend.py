from collections.abc import Sequence
from io import BytesIO

import pikepdf

from pdfmasker.backends.pikepdf.content import (
    Replacement,
    extract_segments,
    iter_content_targets,
    process,
    write_back,
)
from pdfmasker.backends.pikepdf.fonts import collect_fonts
from pdfmasker.base import EditorResult, PlanEntry, RenderMode, TextView
from pdfmasker.errors import MaskerBackendError


def _to_replacement(entry: PlanEntry) -> Replacement:
    """Map a plan entry onto the engine's `Replacement`: per-char fill vs whole-span string."""
    substitution = entry.substitution
    if substitution.mode is RenderMode.PER_CHAR:
        return Replacement(search=entry.target, mask_char=substitution.fill, replace=None)
    return Replacement(search=entry.target, mask_char=None, replace=substitution.fill)


class PikepdfBackend:
    """Handle any PDF in-process; scanned inputs are routed away upstream."""

    def applies_to(self, data: bytes) -> bool:
        """Accept anything that looks like a PDF."""
        return data.startswith(b"%PDF")

    def open(self, data: bytes) -> "PikepdfDocument":
        """Parse the PDF once and return a handle for extract/edit."""
        try:
            pdf = pikepdf.open(BytesIO(data))
        except pikepdf.PdfError as exc:
            error_message = "Could not open PDF"
            raise MaskerBackendError(error_message) from exc
        return PikepdfDocument(data, pdf)


class PikepdfDocument:
    """A single opened PDF, reused for text extraction and editing."""

    def __init__(self, data: bytes, pdf: pikepdf.Pdf) -> None:
        """Hold the original bytes (to return unchanged when nothing matches) and the open PDF."""
        self._data = data
        self._pdf = pdf

    def extract_text(self) -> TextView:
        """Reconstruct the document's text as one string per content-stream segment."""
        segments: list[str] = []
        for container, resources in iter_content_targets(self._pdf):
            fonts = collect_fonts(resources)
            segments.extend(extract_segments(container, fonts))
        return TextView(segments=tuple(segments))

    def edit(self, plan: Sequence[PlanEntry]) -> EditorResult:
        """Apply every plan entry across all content streams and return masked bytes plus counts.

        When nothing matched, the original bytes are returned untouched so a masked document is never re-serialized
        needlessly.
        """
        replacements = [_to_replacement(entry) for entry in plan]
        counts = {replacement.search: 0 for replacement in replacements}
        any_changed = False
        for container, resources in iter_content_targets(self._pdf):
            fonts = collect_fonts(resources)
            instructions, stream_counts, changed = process(container, fonts, replacements)
            for target, count in stream_counts.items():
                counts[target] += count
            if changed:
                write_back(self._pdf, container, instructions)
                any_changed = True
        if not any_changed:
            return EditorResult(document_content=self._data, counts=counts)
        output = BytesIO()
        try:
            self._pdf.save(output)
        except pikepdf.PdfError as exc:
            error_message = "Could not write masked PDF"
            raise MaskerBackendError(error_message) from exc
        return EditorResult(document_content=output.getvalue(), counts=counts)

    def close(self) -> None:
        """Release the open PDF."""
        self._pdf.close()
