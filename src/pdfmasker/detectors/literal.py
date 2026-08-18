from collections.abc import Sequence

from pdfmasker.base import Detection, TextView


class LiteralDetector:
    """Treat each given pattern as a literal target to mask.

    It needs no text at all, since the caller already named the exact strings.
    """

    def __init__(self, patterns: Sequence[str], kind: str = "literal") -> None:
        """Mask each non-blank pattern, tagging matches with `kind`."""
        self._patterns = [pattern for pattern in patterns if pattern and pattern.strip()]
        self._kind = kind

    def detect(self, view: TextView) -> list[Detection]:  # noqa: ARG002  # literals need no text
        """Return one detection per configured pattern, ignoring the document text."""
        return [Detection(text=pattern, kind=self._kind) for pattern in self._patterns]
