import pytest

from pdfmasker import InMemorySubstitutionStore, Masker
from pdfmasker.base import Detection
from pdfmasker.editors.faker import FakerEditor


def test_faker_editor_masks_and_stays_consistent(files_for_test):
    store = InMemorySubstitutionStore()
    masker = Masker(editor=FakerEditor(store, seed=1234))
    data = files_for_test["adp_paystub_hermion_granger.pdf"].content

    first = masker.mask(data, patterns=["HERMIONE GRANGER"])
    assert first.document_content.startswith(b"%PDF-")
    assert first.entries[0].count == 2
    fake = first.entries[0].replacement
    assert fake
    assert fake != "HERMIONE GRANGER"

    # A shared store gives the same target the same fake value on the next document.
    second = masker.mask(data, patterns=["HERMIONE GRANGER"])
    assert second.entries[0].replacement == fake
    assert dict(store.items())["HERMIONE GRANGER"] == fake


def test_faker_editor_seed_is_reproducible():
    one = FakerEditor(InMemorySubstitutionStore(), seed=42).edit(Detection("John Doe"))
    two = FakerEditor(InMemorySubstitutionStore(), seed=42).edit(Detection("John Doe"))
    assert one.fill == two.fill


def test_faker_editor_kind_selects_provider():
    editor = FakerEditor(InMemorySubstitutionStore(), seed=7, providers={"email": "email"})
    substitution = editor.edit(Detection("someone@example.com", kind="email"))
    assert "@" in substitution.fill


def test_faker_editor_rejects_unknown_provider():
    with pytest.raises(ValueError, match="provider"):
        FakerEditor(InMemorySubstitutionStore(), default_provider="not_a_real_provider")
