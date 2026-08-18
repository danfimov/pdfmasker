from pdfmasker import (
    InMemorySubstitutionStore,
    LiteralDetector,
    Masker,
    PseudonymizeEditor,
    RegexDetector,
)


def test_detector_without_patterns_masks_matches(files_for_test):
    data = files_for_test["adp_paystub_hermion_granger.pdf"].content
    masker = Masker(detectors=[RegexDetector(r"HERMIONE", kind="name")])
    result = masker.mask(data)
    assert result.entries[0].count == 2
    assert result.document_content.startswith(b"%PDF-")
    entry = next(entry for entry in result.entries if entry.target == "HERMIONE")
    assert entry.kind == "name"


def test_literal_detector_matches_patterns_argument(files_for_test):
    data = files_for_test["simple_paystub.pdf"].content
    masker = Masker(detectors=[LiteralDetector(["Lorraine Freddie"], kind="person")])
    result = masker.mask(data)
    assert result.entries[0].count == 1
    entry = next(entry for entry in result.entries if entry.target == "Lorraine Freddie")
    assert entry.kind == "person"


def test_shared_store_keeps_pseudonyms_consistent_across_documents(files_for_test):
    store = InMemorySubstitutionStore()
    masker = Masker(editor=PseudonymizeEditor(store))

    first = masker.mask(files_for_test["adp_paystub_hermion_granger.pdf"].content, patterns=["HERMIONE"])
    second = masker.mask(files_for_test["adp_paystub_hermion_granger.pdf"].content, patterns=["HERMIONE"])

    first_pseudonym = next(entry.replacement for entry in first.entries if entry.target == "HERMIONE")
    second_pseudonym = next(entry.replacement for entry in second.entries if entry.target == "HERMIONE")
    assert first_pseudonym == second_pseudonym
    assert dict(store.items())["HERMIONE"] == first_pseudonym
