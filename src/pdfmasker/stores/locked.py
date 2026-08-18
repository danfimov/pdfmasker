import threading
from collections.abc import Callable, Iterable

from pdfmasker.stores.memory import InMemorySubstitutionStore


class LockedSubstitutionStore(InMemorySubstitutionStore):
    """A store safe to share across threads.

    Masking itself is CPU-bound Python and does not get faster under threads (the GIL); this exists for correctness
    when concurrent code shares one mapping, and for free-threaded builds.
    """

    def __init__(self) -> None:
        """Guard the inherited mapping with a per-instance lock."""
        super().__init__()
        self._lock = threading.Lock()

    def get_or_assign(self, key: str, generate: Callable[[], str]) -> str:
        """Assign under the lock so concurrent callers can't race on the same key."""
        with self._lock:
            return super().get_or_assign(key, generate)

    def items(self) -> Iterable[tuple[str, str]]:
        """Return a snapshot taken under the lock, safe to iterate while others mutate."""
        with self._lock:
            return list(super().items())

    def preload(self, mapping: Iterable[tuple[str, str]]) -> None:
        """Seed under the lock."""
        with self._lock:
            super().preload(mapping)
