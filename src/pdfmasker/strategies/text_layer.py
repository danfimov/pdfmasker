import json
import subprocess
from collections.abc import Sequence

from pdfmasker._binary import binary_path
from pdfmasker.errors import MaskError
from pdfmasker.strategies.base import MaskResult, MaskStrategy


class TextLayerStrategy(MaskStrategy):
    """Mask text present in the PDF's text layer via the masker binary."""

    def applies_to(self, data: bytes) -> bool:
        """Return True for anything that looks like a PDF; scan detection routes away upstream."""
        return data.startswith(b"%PDF")

    def mask(
        self,
        data: bytes,
        patterns: Sequence[str],
        mask_with: str | None = None,
    ) -> MaskResult:
        """Mask patterns by driving the masker binary over a subprocess pipe."""
        cleaned_patterns = [pattern for pattern in patterns if pattern and pattern.strip()]
        if not cleaned_patterns:
            error_message = "At least one non-empty pattern is required"
            raise MaskError(error_message)
        if not data:
            error_message = "Source PDF is empty"
            raise MaskError(error_message)

        argv = [binary_path(), "-patterns", json.dumps(cleaned_patterns, ensure_ascii=False)]
        if mask_with is not None:
            argv += ["-mask", mask_with]

        try:
            proc = subprocess.run(  # noqa: S603 - argv is fully controlled
                argv,
                input=data,
                capture_output=True,
                check=False,
            )
        except OSError as exc:
            error_message = "Failed to run masking binary"
            raise MaskError(error_message) from exc

        report = _parse_report(proc.stderr)

        if proc.returncode != 0:
            detail = report.get("error") or proc.stderr.decode("utf-8", "replace").strip()
            raise MaskError(detail or f"masking binary exited with {proc.returncode}")

        return MaskResult(
            pdf=proc.stdout,
            counts=report.get("applied", {}),
        )


def _parse_report(stderr: bytes) -> dict:
    """Parse the JSON status document the binary writes to stderr.

    Returns an empty dict if stderr is not valid JSON, so a garbled report never
    masks the real (non-zero exit) failure.
    """
    if not stderr:
        return {}
    try:
        parsed = json.loads(stderr)
    except (json.JSONDecodeError, UnicodeDecodeError):
        return {}
    return parsed if isinstance(parsed, dict) else {}
