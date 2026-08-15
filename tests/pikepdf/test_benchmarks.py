import pytest

from pdfmasker import Masker
from pdfmasker.strategies.pikepdf import PikepdfTextLayerStrategy


@pytest.mark.benchmark
def test_mask_all_fixtures_pikepdf(files_for_test):
    masker = Masker(strategies=[PikepdfTextLayerStrategy()])
    for file in files_for_test.values():
        masker.mask(file.content, patterns=file.sensitive_keys)
