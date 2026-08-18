import re

from pdfmasker.base import Detection, Span, TextView


class RegexDetector:
    """Mask every match of a regular expression found in the document text."""

    def __init__(self, pattern: str, kind: str = "regex", flags: int = 0) -> None:
        """Compile `pattern` with `flags`; tag matches with `kind`."""
        self._regex = re.compile(pattern, flags)
        self._kind = kind

    def detect(self, view: TextView) -> list[Detection]:
        """Return a detection for each regex match in each segment."""
        detections: list[Detection] = []
        for segment in view.segments:
            for match in self._regex.finditer(segment):
                text = match.group(0)
                if not text:
                    continue
                detections.append(
                    Detection(text=text, kind=self._kind, span=Span(match.start(), match.end())),
                )
        return detections
