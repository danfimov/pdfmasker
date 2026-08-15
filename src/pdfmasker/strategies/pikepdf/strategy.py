"""In-process masking strategy built on pikepdf.

A drop-in alternative to the binary-backed strategy that edits the PDF's content
streams directly in Python, avoiding the per-call subprocess overhead. pikepdf is
an optional dependency; the package `__init__` turns a missing install into a
helpful error, so this module can assume it is present.
"""

from collections.abc import Sequence
from io import BytesIO

import pikepdf

from pdfmasker.errors import MaskError
from pdfmasker.strategies.base import MaskResult, MaskStrategy
from pdfmasker.strategies.pikepdf.content import Replacement, iter_content_targets, process, write_back
from pdfmasker.strategies.pikepdf.fonts import collect_fonts

_DEFAULT_MASK_CHAR = "X"


def _build_replacements(patterns: list[str], mask_with: str | None) -> list[Replacement]:
    """Turn patterns into replacements: a default per-glyph X mask, or a custom whole-span mask."""
    if mask_with is None:
        return [Replacement(search=p, mask_char=_DEFAULT_MASK_CHAR, replace=None) for p in patterns]
    return [Replacement(search=p, mask_char=None, replace=mask_with) for p in patterns]


class PikepdfTextLayerStrategy(MaskStrategy):
    """Mask text in the PDF's text layer in-process using pikepdf."""

    def applies_to(self, data: bytes) -> bool:
        """Accept anything that looks like a PDF; scanned inputs are routed away upstream."""
        return data.startswith(b"%PDF")

    def mask(
        self,
        data: bytes,
        patterns: Sequence[str],
        mask_with: str | None = None,
    ) -> MaskResult:
        """Mask patterns by rewriting the PDF's content streams in-process.

        A missing replacement defaults to a run of `X` the length of each pattern,
        which stays clear of the patterns themselves so a second pass is a no-op.
        """
        cleaned_patterns = [pattern for pattern in patterns if pattern and pattern.strip()]
        if not cleaned_patterns:
            error_message = "At least one non-empty pattern is required"
            raise MaskError(error_message)
        if not data:
            error_message = "Source PDF is empty"
            raise MaskError(error_message)

        replacements = _build_replacements(cleaned_patterns, mask_with)

        try:
            pdf = pikepdf.open(BytesIO(data))
        except pikepdf.PdfError as exc:
            error_message = "Could not open PDF"
            raise MaskError(error_message) from exc

        counts = dict.fromkeys(cleaned_patterns, 0)
        try:
            for kind, container, resources in iter_content_targets(pdf):
                fonts = collect_fonts(resources)
                instructions, stream_counts, changed = process(container, fonts, replacements)
                for pattern, count in stream_counts.items():
                    counts[pattern] += count
                if changed:
                    write_back(pdf, kind, container, instructions)

            output = BytesIO()
            pdf.save(output)
        finally:
            pdf.close()

        return MaskResult(pdf=output.getvalue(), counts=counts)
