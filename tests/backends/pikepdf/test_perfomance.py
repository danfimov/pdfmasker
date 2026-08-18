import pytest


@pytest.mark.benchmark
def test_mask_all_fixtures_pikepdf(files_for_test, pikepdf_masker):
    for file in files_for_test.values():
        pikepdf_masker.mask(file.content, patterns=file.sensitive_keys)


@pytest.mark.limit_memory("8 MB")
def test_masking_stays_within_memory_budget(files_for_test, pikepdf_masker):
    for file in files_for_test.values():
        pikepdf_masker.mask(file.content, patterns=file.sensitive_keys)
