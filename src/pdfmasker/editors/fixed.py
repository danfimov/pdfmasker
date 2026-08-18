from pdfmasker.base import Detection, RenderMode, Substitution


class FixedCharEditor:
    """Mask each glyph with one character, preserving length and layout."""

    def __init__(self, char: str = "X") -> None:
        """Use character as the per-glyph fill."""
        self._char = char

    def edit(self, _: Detection) -> Substitution:
        """Return a per-glyph fill; the matched text and its length are irrelevant here."""
        return Substitution(mode=RenderMode.PER_CHAR, fill=self._char)


class FixedStringEditor:
    """Replace the whole match with one constant string.

    The replacement is emitted contiguously, so a multi-word target rendered across several operators stays in one
    piece rather than being spread over their slots.
    """

    def __init__(self, replacement: str) -> None:
        """Use replacement text as the whole-span replacement for every match."""
        self._replacement = replacement

    def edit(self, _: Detection) -> Substitution:
        """Return the constant string as a whole-span substitution."""
        return Substitution(mode=RenderMode.WHOLE_SPAN, fill=self._replacement)
