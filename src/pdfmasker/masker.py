from collections.abc import Sequence

from pdfmasker.backends.pikepdf import PikepdfBackend
from pdfmasker.base import (
    Backend,
    Detection,
    Detector,
    Editor,
    MaskResult,
    PlanEntry,
    RenderMode,
    Substitution,
    TargetEntry,
    TextView,
)
from pdfmasker.detectors import LiteralDetector
from pdfmasker.editors import FixedCharEditor
from pdfmasker.errors import (
    MaskerBackendError,
    MaskerDetectorError,
    MaskerEditorError,
    MaskError,
    PdfMaskerError,
)


class Masker:
    """Route a PDF to a backend and mask what patterns and detectors turn up."""

    def __init__(
        self,
        *,
        backends: Sequence[Backend] | None = None,
        detectors: Sequence[Detector] | None = None,
        editor: Editor | None = None,
    ) -> None:
        """Configure the roles; the defaults make `Masker().mask(data, patterns=[...])` a plain literal masker."""
        self.backends: list[Backend] = list(backends) if backends is not None else [PikepdfBackend()]
        self.detectors: list[Detector] = list(detectors) if detectors is not None else []
        self.editor: Editor = editor if editor is not None else FixedCharEditor()

    def mask(self, data: bytes, patterns: Sequence[str] | None = None) -> MaskResult:
        """Mask `data`, combining literal `patterns` with any configured detectors.

        Raises `MaskError` if the PDF is empty, if neither patterns nor detectors are given, or if no backend accepts
        the input.
        """
        if not data:
            error_message = "Source PDF is empty"
            raise MaskError(error_message)
        literals = [pattern for pattern in (patterns or []) if pattern and pattern.strip()]
        if not literals and not self.detectors:
            error_message = "At least one non-empty pattern or detector is required"
            raise MaskError(error_message)

        backend = self._select(data)
        document = backend.open(data)
        try:
            detections: list[Detection] = []
            if literals:
                detections.extend(LiteralDetector(literals).detect(TextView(())))
            if self.detectors:
                view = document.extract_text()
                for detector in self.detectors:
                    detections.extend(self._run_detector(detector, view))
            plan = self._build_plan(detections)
            result = document.edit(plan)
        finally:
            document.close()

        entries = [
            TargetEntry(
                target=entry.target,
                replacement=self._render(entry),
                kind=entry.kind,
                count=result.counts.get(entry.target, 0),
            )
            for entry in plan
        ]
        return MaskResult(document_content=result.document_content, entries=entries)

    def _select(self, data: bytes) -> Backend:
        for backend in self.backends:
            if backend.applies_to(data):
                return backend
        error_message = "No backend could handle this PDF"
        raise MaskerBackendError(error_message)

    @staticmethod
    def _run_detector(detector: Detector, view: TextView) -> list[Detection]:
        """Run one detector, tagging any failure of this plugin as a detector error.

        Detections are materialized here so a lazily-raised failure is caught; our own errors propagate unwrapped,
        only a foreign exception is re-tagged.
        """
        try:
            return list(detector.detect(view))
        except PdfMaskerError:
            raise
        except Exception as exc:
            error_message = f"detector {type(detector).__name__} failed"
            raise MaskerDetectorError(error_message) from exc

    def _build_plan(self, detections: Sequence[Detection]) -> list[PlanEntry]:
        """Resolve detections to plan entries, one per distinct target (first detection wins)."""
        seen: dict[str, PlanEntry] = {}
        for detection in detections:
            if detection.text in seen:
                continue
            seen[detection.text] = PlanEntry(
                target=detection.text,
                substitution=self._render_substitution(detection),
                kind=detection.kind,
            )
        return list(seen.values())

    def _render_substitution(self, detection: Detection) -> Substitution:
        """Ask the editor for a substitution, tagging any failure of this plugin.

        The substitution store raises its own `MaskerEditorError`, which propagates unwrapped; only a foreign
        exception from a custom editor is re-tagged.
        """
        try:
            return self.editor.edit(detection)
        except PdfMaskerError:
            raise
        except Exception as exc:
            error_message = f"editor {type(self.editor).__name__} failed on {detection.text!r}"
            raise MaskerEditorError(error_message) from exc

    @staticmethod
    def _render(entry: PlanEntry) -> str:
        """Render an entry's substitution for the audit log the way the backend renders it."""
        substitution = entry.substitution
        if substitution.mode is RenderMode.WHOLE_SPAN:
            return substitution.fill
        return substitution.fill * len(entry.target)


_default_masker = Masker()


def mask_pdf(
    data: bytes,
    patterns: Sequence[str] | None = None,
    detectors: Sequence[Detector] | None = None,
    editor: Editor | None = None,
) -> MaskResult:
    """Mask the given PDF bytes with the default pipeline.

    Args:
        data: Source PDF bytes.
        patterns: Literal strings to mask.
        detectors: Optional detectors that discover further targets from the text.
        editor: Optional render policy; defaults to a per-glyph `X` mask.

    Returns:
        The masked PDF and a per-target log of what was replaced.

    Raises:
        MaskError: if masking fails.
    """
    if editor is not None or detectors is not None:
        masker = Masker(detectors=detectors, editor=editor)
    else:
        masker = _default_masker
    return masker.mask(data, patterns=patterns)
