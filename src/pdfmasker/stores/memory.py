from collections.abc import Callable, Iterable

from pdfmasker.errors import MaskerEditorError


class InMemorySubstitutionStore:
    """A dict-backed store with a reverse index guaranteeing distinct values.

    Assignment is first-come: whichever key asks first claims a value, and later keys re-roll until they get an unused
    one. That makes results depend on key order, which is the price of the collision-free guarantee; a keyed-derivation
    editor is the way to trade that back for order-independence.
    """

    MAX_ASSIGN_ATTEMPTS = 100

    def __init__(self) -> None:
        """Start with an empty mapping and an empty set of taken values."""
        self._by_key: dict[str, str] = {}
        self._taken: set[str] = set()

    def get_or_assign(self, key: str, generate: Callable[[], str]) -> str:
        """Return the value mapped to `key`, assigning a fresh unused one if absent.

        `generate` must vary its output across calls so a collision can be resolved; the built-in editors do
        (a counter or a salt). A generator that keeps returning taken values raises `MaskerEditorError` after a
        bounded number of attempts rather than looping forever.
        """
        existing = self._by_key.get(key)
        if existing is not None:
            return existing
        for _ in range(self.MAX_ASSIGN_ATTEMPTS):
            value = generate()
            if value not in self._taken:
                self._by_key[key] = value
                self._taken.add(value)
                return value
        error_message = "substitution generator exhausted without a free value"
        raise MaskerEditorError(error_message)

    def items(self) -> Iterable[tuple[str, str]]:
        """Iterate the current key-to-replacement mapping."""
        return self._by_key.items()

    def preload(self, mapping: Iterable[tuple[str, str]]) -> None:
        """Seed known mappings so a later document reuses an earlier one's substitutions.

        Existing keys are kept; every seeded value is marked taken so fresh assignments still avoid it.
        """
        for key, value in mapping:
            self._by_key.setdefault(key, value)
            self._taken.add(value)
