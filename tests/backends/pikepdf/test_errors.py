import pytest

from pdfmasker import (
    InMemorySubstitutionStore,
    Masker,
    MaskerBackendError,
    MaskerDetectorError,
    MaskerEditorError,
    MaskError,
)


class _BoomDetector:
    def detect(self, view):
        raise ValueError("boom")


class _BoomEditor:
    def edit(self, detection):
        raise ValueError("boom")


class _RejectBackend:
    def applies_to(self, data):
        return False


def test_backend_open_failure_raises_backend_error(pikepdf_masker):
    with pytest.raises(MaskerBackendError):
        pikepdf_masker.mask(b"%PDF-1.7 not actually a pdf", patterns=["x"])


def test_no_applicable_backend_raises_backend_error():
    masker = Masker(backends=[_RejectBackend()])  # ty: ignore[invalid-argument-type]
    with pytest.raises(MaskerBackendError):
        masker.mask(b"%PDF-1.7 whatever", patterns=["x"])


def test_detector_failure_raises_detector_error(files_for_test):
    masker = Masker(detectors=[_BoomDetector()])
    with pytest.raises(MaskerDetectorError):
        masker.mask(files_for_test["simple_paystub.pdf"].content)


def test_editor_failure_raises_editor_error(files_for_test):
    masker = Masker(editor=_BoomEditor())
    with pytest.raises(MaskerEditorError):
        masker.mask(files_for_test["simple_paystub.pdf"].content, patterns=["Lorraine Freddie"])


def test_store_exhaustion_raises_editor_error():
    store = InMemorySubstitutionStore()
    store.get_or_assign("first", lambda: "X")
    with pytest.raises(MaskerEditorError):
        store.get_or_assign("second", lambda: "X")  # generator never yields a free value


def test_role_errors_are_mask_errors(files_for_test):
    masker = Masker(detectors=[_BoomDetector()])
    with pytest.raises(MaskError):  # base class still catches the narrowed error
        masker.mask(files_for_test["simple_paystub.pdf"].content)
