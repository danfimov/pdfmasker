import pytest

from pdfmasker import FixedStringEditor, Masker, MaskError, MaskResult


def test_masks_cid_font_pdf(pikepdf_masker, files_for_test):
    result = pikepdf_masker.mask(files_for_test["simple_paystub.pdf"].content, patterns=["Lorraine Freddie"])
    assert isinstance(result, MaskResult)
    assert result.document_content.startswith(b"%PDF-")
    assert result.entries[0].count == 1


def test_masks_object_stream_pdf(pikepdf_masker, files_for_test):
    data = files_for_test["adp_paystub_hermion_granger.pdf"].content
    result = pikepdf_masker.mask(data, patterns=["HERMIONE", "GRANGER"])
    assert result.document_content.startswith(b"%PDF-")
    assert {entry.target: entry.count for entry in result.entries} == {"HERMIONE": 2, "GRANGER": 2}


@pytest.mark.parametrize(
    ("fixture", "name"),
    [
        ("paychex_paystub_bill_weasley.pdf", "Bill Weasley"),
        ("adp_paystub_neville_lestrange-weasley.pdf", "NEVILLE LESTRANGE-WEASLEY"),
    ],
)
def test_masks_full_name_split_across_operators(pikepdf_masker, files_for_test, fixture, name):
    result = pikepdf_masker.mask(files_for_test[fixture].content, patterns=[name])
    assert result.entries[0].count == 2


@pytest.mark.parametrize("pattern", ["lorraine freddie", "LORRAINE FREDDIE", "Lorraine FREDDIE"])
def test_case_insensitive_multiword(pikepdf_masker, files_for_test, pattern):
    result = pikepdf_masker.mask(files_for_test["simple_paystub.pdf"].content, patterns=[pattern])
    assert result.entries[0].count == 1


def test_absent_pattern_reports_zero_and_leaves_pdf_valid(pikepdf_masker, files_for_test):
    result = pikepdf_masker.mask(files_for_test["simple_paystub.pdf"].content, patterns=["Nonexistent Person"])
    assert result.entries[0].count == 0
    assert result.document_content.startswith(b"%PDF-")


def test_custom_mask_string_is_applied(files_for_test):
    data = files_for_test["adp_paystub_hermion_granger.pdf"].content
    masker = Masker(editor=FixedStringEditor("####"))
    result = masker.mask(data, patterns=["HERMIONE"])
    assert result.entries[0].count == 2
    assert result.document_content.startswith(b"%PDF-")
    entry = next(entry for entry in result.entries if entry.target == "HERMIONE")
    assert entry.replacement == "####"


def test_masking_is_idempotent(pikepdf_masker, files_for_test):
    once = pikepdf_masker.mask(
        files_for_test["adp_paystub_hermion_granger.pdf"].content, patterns=["HERMIONE", "GRANGER"]
    )
    twice = pikepdf_masker.mask(once.document_content, patterns=["HERMIONE", "GRANGER"])
    assert {entry.target: entry.count for entry in twice.entries} == {"HERMIONE": 0, "GRANGER": 0}
    assert twice.document_content.startswith(b"%PDF-")


def test_empty_patterns_raise(pikepdf_masker, files_for_test):
    with pytest.raises(MaskError):
        pikepdf_masker.mask(files_for_test["simple_paystub.pdf"].content, patterns=["   "])


def test_empty_pdf_raises(pikepdf_masker):
    with pytest.raises(MaskError):
        pikepdf_masker.mask(b"", patterns=["anything"])
