import pytest

from pdfmasker import mask_pdf


@pytest.mark.benchmark
def test_mask_all_fixtures(files_for_test):
    for file in files_for_test.values():
        mask_pdf(file.content, patterns=file.sensitive_keys)
