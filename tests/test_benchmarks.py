import pytest


@pytest.mark.benchmark
def test_mask_all_fixtures(files_for_test, text_layer_masker):
    for file in files_for_test.values():
        text_layer_masker.mask(file.content, patterns=file.sensitive_keys)
