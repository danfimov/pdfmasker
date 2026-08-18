from collections.abc import Mapping, Sequence

from pdfmasker.base import Detection, RenderMode, Substitution, SubstitutionStore
from pdfmasker.errors import MissingDependencyError
from pdfmasker.stores import InMemorySubstitutionStore

try:
    from faker import Faker
except ModuleNotFoundError as exc:
    if exc.name != "faker":
        raise
    _message = "faker is not installed. Install it with: pip install pdfmasker[faker]"
    raise MissingDependencyError(_message) from exc


class FakerEditor:
    """Replace each distinct target with a realistic fake value drawn from Faker."""

    def __init__(
        self,
        store: SubstitutionStore | None = None,
        locale: str | Sequence[str] | dict[str, int | float] | None = None,
        seed: int | None = None,
        providers: Mapping[str, str] | None = None,
        default_provider: str = "name",
    ) -> None:
        """Build a Faker instances and validate providers."""
        self._store = store if store is not None else InMemorySubstitutionStore()
        self._faker = Faker(locale)
        if seed is not None:
            self._faker.seed_instance(seed)
        self._providers = dict(providers or {})
        self._default = default_provider
        for provider in (*self._providers.values(), self._default):
            if not hasattr(self._faker, provider):
                error_message = f"Faker has no provider {provider!r}"
                raise ValueError(error_message)

    def edit(self, detection: Detection) -> Substitution:
        """Produce a fake replacement for the match."""
        provider = self._providers.get(detection.kind, self._default)
        value = self._store.get_or_assign(detection.text, lambda: str(getattr(self._faker, provider)()))
        return Substitution(mode=RenderMode.WHOLE_SPAN, fill=value)
