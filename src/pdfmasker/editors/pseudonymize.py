import hashlib
import hmac
import itertools

from pdfmasker.base import Detection, RenderMode, Substitution, SubstitutionStore
from pdfmasker.stores import InMemorySubstitutionStore


class PseudonymizeEditor:
    """Replace each distinct target with a stable, unique pseudonym from a store.

    The same target always yields the same pseudonym, and no two targets share one, because the store owns the
    mapping and its collision check.
    """

    def __init__(self, store: SubstitutionStore | None = None, prefix: str = "PERSON") -> None:
        """Assign a stable pseudonym to each distinct target."""
        self._store = store if store is not None else InMemorySubstitutionStore()
        self._prefix = prefix
        self._counter = itertools.count(1)

    def edit(self, detection: Detection) -> Substitution:
        """Look up or mint a pseudonym for the detected text."""
        value = self._store.get_or_assign(
            detection.text,
            lambda: f"{self._prefix}_{next(self._counter)}",
        )
        return Substitution(mode=RenderMode.WHOLE_SPAN, fill=value)


class KeyedPseudonymizeEditorEditor:
    """Derive a pseudonym deterministically from the target with a keyed hash.

    With no store the pseudonym is a pure function of the target, so it is identical across runs and safe to compute
    in parallel — at the cost of possible hash collisions. Passing a store restores the collision-free guarantee by
    re-deriving with a salt when a truncated digest clashes, which reintroduces order-dependence only on the clashing
    keys.
    """

    def __init__(
        self,
        key: bytes,
        store: SubstitutionStore | None = None,
        prefix: str = "ID",
        length: int = 10,
    ) -> None:
        """Pass a `store` to trade parallel-safety for a collision-free guarantee."""
        self._key = key
        self._store = store
        self._prefix = prefix
        self._length = length

    def _derive(self, text: str) -> str:
        digest = hmac.new(self._key, text.encode("utf-8"), hashlib.sha256).hexdigest()
        return f"{self._prefix}_{digest[: self._length]}"

    def edit(self, detection: Detection) -> Substitution:
        """Return the derived pseudonym, resolving digest clashes through the store if present."""
        if self._store is None:
            return Substitution(mode=RenderMode.WHOLE_SPAN, fill=self._derive(detection.text))
        salt = itertools.count()

        def generate() -> str:
            attempt = next(salt)
            seed = detection.text if attempt == 0 else f"{detection.text}#{attempt}"
            return self._derive(seed)

        value = self._store.get_or_assign(detection.text, generate)
        return Substitution(mode=RenderMode.WHOLE_SPAN, fill=value)
