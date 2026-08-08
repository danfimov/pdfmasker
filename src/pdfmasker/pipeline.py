from collections.abc import Sequence

from pdfmasker.errors import MaskError
from pdfmasker.strategies.base import MaskResult, MaskStrategy
from pdfmasker.strategies.text_layer import TextLayerStrategy


class Masker:
    """Route a PDF to the first strategy that can handle it."""

    def __init__(self, strategies: Sequence[MaskStrategy] | None = None) -> None:
        """Build a masker over `strategies` (defaults to just `TextLayerStrategy`)."""
        self.strategies: list[MaskStrategy] = list(strategies) if strategies is not None else [TextLayerStrategy()]

    def mask(
        self,
        data: bytes,
        patterns: Sequence[str],
        mask_with: str | None = None,
    ) -> MaskResult:
        """Mask patterns in pdf data using the first applicable strategy."""
        for strategy in self.strategies:
            if strategy.applies_to(data):
                return strategy.mask(data, patterns, mask_with)
        error_message = "No masking strategy could handle this PDF"
        raise MaskError(error_message)


_default_masker = Masker()


def mask_pdf(
    data: bytes,
    patterns: Sequence[str],
    mask_with: str | None = None,
) -> MaskResult:
    """Mask patterns in the given PDF bytes using the default pipeline.

    Args:
        data: Source PDF bytes.
        patterns: Text values to mask (e.g. names, SSNs).
        mask_with: Replacement string. `None` uses the backend default (a run  of ``X`` matching each target's length).

    Returns:
        Masked PDF and per-target replacement counts.

    Raises:
        MaskError: if masking fails.

    """
    return _default_masker.mask(data, patterns, mask_with)
