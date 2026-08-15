import json
from dataclasses import dataclass
from pathlib import Path

import pytest

from pdfmasker import Masker
from pdfmasker.strategies.pikepdf import PikepdfTextLayerStrategy
from pdfmasker.strategies.text_layer import TextLayerStrategy


@dataclass(frozen=True)
class FileToMask:
    content: bytes
    sensitive_keys: list[str]


@pytest.fixture(scope="session")
def files_for_test() -> dict[str, FileToMask]:
    files = {}
    fixtures_directory = Path(__file__).resolve().parent / "fixtures"
    for pdf in fixtures_directory.glob("paystubs/*.pdf"):
        keys_path = pdf.parent / f"{pdf.stem}.keys.json"
        sensitive_keys = json.loads(keys_path.read_text()) if keys_path.exists() else []
        files[pdf.name] = FileToMask(content=pdf.read_bytes(), sensitive_keys=sensitive_keys)
    return files


@pytest.fixture(scope="session")
def pikepdf_masker() -> Masker:
    return Masker(strategies=[PikepdfTextLayerStrategy()])


@pytest.fixture(scope="session")
def text_layer_masker() -> Masker:
    return Masker(strategies=[TextLayerStrategy()])
