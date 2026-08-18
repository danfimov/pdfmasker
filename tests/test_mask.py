import pytest

from pdfmasker import FixedStringEditor, MaskError, MaskResult, mask_pdf


def test_masks_simple_pdf_fallback_path(files_for_test: dict):
    result = mask_pdf(files_for_test["simple_paystub.pdf"].content, patterns=["Lorraine Freddie"])
    assert isinstance(result, MaskResult)
    assert result.document_content.startswith(b"%PDF-")
    assert result.entries[0].count == 1


def test_masks_object_stream_pdf_hybrid_path(files_for_test: dict):
    result = mask_pdf(files_for_test["adp_paystub_hermion_granger.pdf"].content, patterns=["HERMIONE", "GRANGER"])
    assert result.document_content.startswith(b"%PDF-")
    assert {entry.target: entry.count for entry in result.entries} == {"HERMIONE": 2, "GRANGER": 2}


def test_absent_pattern_reports_zero_and_leaves_pdf_valid(files_for_test: dict):
    result = mask_pdf(files_for_test["simple_paystub.pdf"].content, patterns=["Nonexistent Person"])
    assert result.entries[0].count == 0
    assert result.document_content.startswith(b"%PDF-")


def test_custom_mask_string_is_applied(files_for_test: dict):
    data = files_for_test["adp_paystub_hermion_granger.pdf"].content
    result = mask_pdf(data, patterns=["HERMIONE"], editor=FixedStringEditor("####"))
    assert result.entries[0].count == 2
    assert result.document_content.startswith(b"%PDF-")


def test_empty_patterns_raise(files_for_test: dict):
    with pytest.raises(MaskError):
        mask_pdf(files_for_test["simple_paystub.pdf"].content, patterns=["   "])


def test_empty_pdf_raises():
    with pytest.raises(MaskError):
        mask_pdf(b"", patterns=["anything"])
