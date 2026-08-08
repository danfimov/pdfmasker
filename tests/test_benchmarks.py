import pytest

from pdfmasker import mask_pdf


@pytest.mark.benchmark
def test_mask_all_fixtures(files_for_test: dict[str, bytes]) -> None:
    patterns = ["Lorraine Freddie", "ANTWANE", "JEFFERSON-TOLBERT"]  # Union of names present across the fixtures
    for data in files_for_test.values():
        mask_pdf(data, patterns=patterns)
