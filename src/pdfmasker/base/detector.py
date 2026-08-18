from collections.abc import Iterable
from typing import Protocol, runtime_checkable

from pdfmasker.base.types import Detection, TextView


@runtime_checkable
class Detector(Protocol):
    """Decide what to mask from the document's text."""

    def detect(self, view: TextView) -> Iterable[Detection]:
        """Yield a detection for each span that should be masked."""
        ...
