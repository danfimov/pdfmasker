from collections.abc import Sequence
from typing import Protocol, runtime_checkable

from pdfmasker.base.types import EditorResult, PlanEntry, TextView


@runtime_checkable
class Document(Protocol):
    """A parsed document handle, so extraction and editing share one parse."""

    def extract_text(self) -> TextView:
        """Return the document's text; called only when a detector needs it."""
        ...

    def edit(self, plan: Sequence[PlanEntry]) -> EditorResult:
        """Apply the plan and return the masked bytes with per-target counts."""
        ...

    def close(self) -> None:
        """Release the parsed document."""
        ...


@runtime_checkable
class Backend(Protocol):
    """An implementation of logic that can read and rewrite documents document."""

    def applies_to(self, data: bytes) -> bool:
        """Return True if this backend can handle the given bytes."""
        ...

    def open(self, data: bytes) -> Document:
        """Parse the document once and return a handle for extract/edit."""
        ...
