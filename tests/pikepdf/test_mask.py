"""End-to-end tests for the in-process pikepdf strategy (mirror of tests/test_mask.py)."""

import importlib
import sys

import pytest

from pdfmasker import Masker, MaskError, MaskResult
from pdfmasker.errors import MissingDependencyError
from pdfmasker.strategies.pikepdf import PikepdfTextLayerStrategy


@pytest.fixture
def masker() -> Masker:
    return Masker(strategies=[PikepdfTextLayerStrategy()])


def test_masks_cid_font_pdf(masker, files_for_test):
    result = masker.mask(files_for_test["simple_paystub.pdf"].content, patterns=["Lorraine Freddie"])
    assert isinstance(result, MaskResult)
    assert result.pdf.startswith(b"%PDF-")
    assert result.counts["Lorraine Freddie"] == 1


def test_masks_object_stream_pdf(masker, files_for_test):
    data = files_for_test["adp_paystub_hermion_granger.pdf"].content
    result = masker.mask(data, patterns=["HERMIONE", "GRANGER"])
    assert result.pdf.startswith(b"%PDF-")
    assert result.counts == {"HERMIONE": 2, "GRANGER": 2}


@pytest.mark.parametrize(
    ("fixture", "name"),
    [
        ("paychex_paystub_bill_weasley.pdf", "Bill Weasley"),
        ("adp_paystub_neville_lestrange-weasley.pdf", "NEVILLE LESTRANGE-WEASLEY"),
    ],
)
def test_masks_full_name_split_across_operators(masker, files_for_test, fixture, name):
    result = masker.mask(files_for_test[fixture].content, patterns=[name])
    assert result.counts[name] == 2


@pytest.mark.parametrize("pattern", ["lorraine freddie", "LORRAINE FREDDIE", "Lorraine FREDDIE"])
def test_case_insensitive_multiword(masker, files_for_test, pattern):
    result = masker.mask(files_for_test["simple_paystub.pdf"].content, patterns=[pattern])
    assert result.counts[pattern] == 1


def test_absent_pattern_reports_zero_and_leaves_pdf_valid(masker, files_for_test):
    result = masker.mask(files_for_test["simple_paystub.pdf"].content, patterns=["Nonexistent Person"])
    assert result.counts["Nonexistent Person"] == 0
    assert result.pdf.startswith(b"%PDF-")


def test_custom_mask_string_is_applied(masker, files_for_test):
    data = files_for_test["adp_paystub_hermion_granger.pdf"].content
    result = masker.mask(data, patterns=["HERMIONE"], mask_with="####")
    assert result.counts["HERMIONE"] == 2
    assert result.pdf.startswith(b"%PDF-")


def test_masking_is_idempotent(masker, files_for_test):
    once = masker.mask(files_for_test["adp_paystub_hermion_granger.pdf"].content, patterns=["HERMIONE", "GRANGER"])
    twice = masker.mask(once.pdf, patterns=["HERMIONE", "GRANGER"])
    assert twice.counts == {"HERMIONE": 0, "GRANGER": 0}
    assert twice.pdf.startswith(b"%PDF-")


def test_empty_patterns_raise(masker, files_for_test):
    with pytest.raises(MaskError):
        masker.mask(files_for_test["simple_paystub.pdf"].content, patterns=["   "])


def test_empty_pdf_raises(masker):
    with pytest.raises(MaskError):
        masker.mask(b"", patterns=["anything"])


class _BlockPikepdf:
    def find_spec(self, name, path=None, target=None):  # noqa: ARG002
        if name == "pikepdf" or name.startswith("pikepdf."):
            raise ModuleNotFoundError(name=name)


def test_missing_pikepdf_raises_helpful_error(monkeypatch):
    monkeypatch.setattr(sys, "meta_path", [_BlockPikepdf(), *sys.meta_path])
    for name in list(sys.modules):
        if name == "pikepdf" or name.startswith(("pikepdf.", "pdfmasker.strategies.pikepdf")):
            monkeypatch.delitem(sys.modules, name, raising=False)
    with pytest.raises(MissingDependencyError, match=r"pip install pdfmasker\[pikepdf\]"):
        importlib.import_module("pdfmasker.strategies.pikepdf")
