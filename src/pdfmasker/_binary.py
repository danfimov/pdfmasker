import os
import sys
from functools import cache
from importlib import resources
from pathlib import Path

from pdfmasker.errors import BinaryNotFoundError

_BINARY_NAME = "masker.exe" if sys.platform == "win32" else "masker"
_ENV_OVERRIDE = "PDFMASKER_BINARY"


@cache
def binary_path() -> str:
    """Return the absolute path to the masker binary.

    Raises:
        BinaryNotFoundError: if no usable binary can be found.

    """
    override = os.environ.get(_ENV_OVERRIDE)
    if override:
        if not Path(override).is_file():
            error_message = f"{_ENV_OVERRIDE}={override!r} does not point to a file"
            raise BinaryNotFoundError(error_message)
        return override

    try:
        with resources.as_file(
            resources.files("pdfmasker").joinpath("_bin", _BINARY_NAME),
        ) as path:
            if path.is_file():
                return str(path)
    except (FileNotFoundError, ModuleNotFoundError):
        pass

    error_message = (
        f"Could not find {_BINARY_NAME!r}; the wheel was built without the "
        f"masking binary, or set {_ENV_OVERRIDE} to a locally built one"
    )
    raise BinaryNotFoundError(error_message)
