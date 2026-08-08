import pytest

from pdfmasker import MaskError, MaskResult, mask_pdf


def test_masks_simple_pdf_fallback_path(files_for_test: dict[str, bytes]):
    result = mask_pdf(files_for_test["test_paystub.pdf"], patterns=["Lorraine Freddie"])
    assert isinstance(result, MaskResult)
    assert result.pdf.startswith(b"%PDF-")
    assert result.counts["Lorraine Freddie"] == 1


def test_masks_object_stream_pdf_hybrid_path(files_for_test: dict[str, bytes]):
    result = mask_pdf(files_for_test["test_paystub_adp_hybrid.pdf"], patterns=["ANTWANE", "JEFFERSON-TOLBERT"])
    assert result.pdf.startswith(b"%PDF-")
    assert result.counts == {"ANTWANE": 2, "JEFFERSON-TOLBERT": 2}


def test_absent_pattern_reports_zero_and_leaves_pdf_valid(files_for_test: dict[str, bytes]):
    result = mask_pdf(files_for_test["test_paystub.pdf"], patterns=["Nonexistent Person"])
    assert result.counts["Nonexistent Person"] == 0
    assert result.pdf.startswith(b"%PDF-")


def test_custom_mask_string_is_applied(files_for_test: dict[str, bytes]):
    result = mask_pdf(files_for_test["test_paystub_adp_hybrid.pdf"], patterns=["ANTWANE"], mask_with="####")
    assert result.counts["ANTWANE"] == 2
    assert result.pdf.startswith(b"%PDF-")


def test_empty_patterns_raise(files_for_test: dict[str, bytes]):
    with pytest.raises(MaskError):
        mask_pdf(files_for_test["test_paystub.pdf"], patterns=["   "])


def test_empty_pdf_raises():
    with pytest.raises(MaskError):
        mask_pdf(b"", patterns=["anything"])
