from pathlib import Path

import pytest

FIXTURES_DIR = Path(__file__).resolve().parent / "fixtures"


@pytest.fixture(scope="session")
def files_for_test() -> dict[str, bytes]:
    """Map each fixture PDF's filename to its raw bytes."""
    return {pdf.name: pdf.read_bytes() for pdf in FIXTURES_DIR.glob("*.pdf")}
