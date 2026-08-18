from collections.abc import Callable, Iterable
from typing import Protocol, runtime_checkable


@runtime_checkable
class SubstitutionStore(Protocol):
    """Keep target-to-replacement mappings consistent and collision-free."""

    def get_or_assign(self, key: str, generate: Callable[[], str]) -> str:
        """Return the value already mapped to key, else assign one from generator function.

        This is the single point that mutates the mapping, so it is where a thread- or process-safe implementation adds
        its lock.
        """
        ...

    def items(self) -> Iterable[tuple[str, str]]:
        """Iterate the current key-to-replacement mapping (for audit and feed-forward)."""
        ...
