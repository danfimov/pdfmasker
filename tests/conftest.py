import json
from dataclasses import dataclass
from pathlib import Path

import pytest

FIXTURES_DIR = Path(__file__).resolve().parent / "fixtures"


@dataclass(frozen=True)
class FileToMask:
    content: bytes
    sensitive_keys: list[str]


@pytest.fixture(scope="session")
def files_for_test() -> dict[str, FileToMask]:
    files = {}
    for pdf in FIXTURES_DIR.glob("paystubs/*.pdf"):
        keys_path = pdf.parent / f"{pdf.stem}.keys.json"
        sensitive_keys = json.loads(keys_path.read_text()) if keys_path.exists() else []
        files[pdf.name] = FileToMask(content=pdf.read_bytes(), sensitive_keys=sensitive_keys)
    return files
