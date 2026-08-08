from abc import ABC, abstractmethod
from collections.abc import Sequence
from dataclasses import dataclass, field


@dataclass(frozen=True)
class MaskResult:
    """The outcome of a masking operation.

    Attributes:
        pdf: The masked PDF as bytes.
        counts: Per-target replacement counts, keyed by the requested pattern.

    """

    pdf: bytes
    counts: dict[str, int] = field(default_factory=dict)


class MaskStrategy(ABC):
    """One approach to masking a PDF."""

    @abstractmethod
    def applies_to(self, data: bytes) -> bool:
        """Return True if this strategy can handle the given PDF bytes."""

    @abstractmethod
    def mask(
        self,
        data: bytes,
        patterns: Sequence[str],
        mask_with: str | None = None,
    ) -> MaskResult:
        """Mask patterns in pdf data and return the result.

        Args:
            data: The source PDF bytes.
            patterns: Text values to mask.
            mask_with: Replacement string. `XXX` by default.

        """
