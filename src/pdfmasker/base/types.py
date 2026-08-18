from collections.abc import Mapping
from dataclasses import dataclass, field
from enum import Enum


class RenderMode(str, Enum):
    """How a matched span is turned into its mask."""

    # fills each matched glyph with a single character, so the token keeps its length and CID byte slots and the visual
    # layout never shifts.
    PER_CHAR = "per_char"

    # emits one replacement string for the whole match, which a differently sized or multi-word substitution needs to
    # stay contiguous.
    WHOLE_SPAN = "whole_span"


@dataclass(frozen=True)
class Span:
    """Where a detection sits in the source, for backends that anchor by position."""

    start: int
    end: int
    page: int | None = None
    bbox: tuple[float, float, float, float] | None = None


@dataclass(frozen=True)
class Detection:
    """One thing a detector wants masked.

    `text` is the only field a string-anchored backend consumes — it re-finds that text in the document. `kind` and
    `score` carry *why* it matched so an editor can render PII types differently and callers can audit the result.
    """

    text: str
    kind: str = "generic"
    score: float = 1.0
    span: Span | None = None


@dataclass(frozen=True)
class Substitution:
    """An editor's answer for one match: the render mode and the char/string to use."""

    mode: RenderMode
    fill: str


@dataclass(frozen=True)
class PlanEntry:
    """A resolved instruction for the backend: find `target`, render it this way."""

    target: str
    substitution: Substitution
    kind: str = "generic"


@dataclass(frozen=True)
class TextView:
    """The document's readable text, as the per-segment strings the backend produced."""

    segments: tuple[str, ...]

    @property
    def text(self) -> str:
        """Join the segments for whole-string scanning; matches across the join are not re-findable."""
        return "\n".join(self.segments)


@dataclass(frozen=True)
class EditorResult:
    """A backend's raw output: the masked bytes and per-target hit counts.

    Counts are produced only by the matcher, so they travel back with the bytes rather than being recomputed by
    the caller.
    """

    document_content: bytes
    counts: Mapping[str, int]


@dataclass(frozen=True)
class TargetEntry:
    """What happened to one target: the string it became, why it matched, how often.

    `replacement` records the rendered value (a pseudonym, or the fill string), which is what a batch feeds forward so
    the next document reuses the same substitution.
    """

    target: str
    replacement: str
    kind: str
    count: int

    def __repr__(self) -> str:
        """Return string representation of target entry."""
        return f"{self.target!r} -> {self.replacement!r} (x{self.count}, kind={self.kind})"


@dataclass(frozen=True)
class MaskResult:
    """The public result of masking: the PDF plus a structured, auditable per-target log."""

    document_content: bytes
    entries: list[TargetEntry] = field(default_factory=list)
