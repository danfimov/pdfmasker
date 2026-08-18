from typing import Protocol, runtime_checkable

from pdfmasker.base.types import Detection, Substitution


@runtime_checkable
class Editor(Protocol):
    """Decide how one match is rendered."""

    def edit(self, detection: Detection, /) -> Substitution:
        """Return the substitution to apply for this detection.

        The parameter is positional-only so editors that ignore the match may name it freely (e.g. `_`).
        """
        ...
