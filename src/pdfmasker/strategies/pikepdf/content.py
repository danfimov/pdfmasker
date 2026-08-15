"""Walk a document's content streams and rewrite the visible text.

Every stream that can carry text is visited: each page and, recursively, its Form
XObjects. Because text is frequently split across many show-text operators, matching
works on a reconstructed segment — a run of consecutive show operators under one
font, bounded by font changes and text-block markers. Matches are found against the
segment's full text, then the replacement is redistributed back over the operators,
re-encoding only the pieces whose characters actually changed.

pikepdf discards edits made to an instruction's operands in place, so a changed
instruction is always rebuilt as a new one.
"""

from collections import defaultdict
from collections.abc import Iterator, Mapping, Sequence
from dataclasses import dataclass

import pikepdf
from pikepdf import ContentStreamInstruction, parse_content_stream

from pdfmasker.strategies.pikepdf.fold import match_flex_at
from pdfmasker.strategies.pikepdf.fonts import Font

_SHOW_SINGLE = {"Tj", "'", '"'}
_TF_MIN_OPERANDS = 2


@dataclass(frozen=True)
class Replacement:
    """One target and how to render its mask.

    A default mask repeats a single character over each matched glyph; a custom mask
    emits its whole string once for the match.
    """

    search: str
    mask_char: str | None
    replace: str | None


@dataclass
class _Chunk:
    """One show-text piece within a segment, tracked back to where its bytes live."""

    font: Font
    text: str
    slots: int
    new_op: bool
    instr_index: int
    element_index: int | None


def iter_content_targets(pdf: pikepdf.Pdf) -> Iterator[tuple[str, pikepdf.Object, pikepdf.Object | None]]:
    """Yield each page and Form XObject together with its resources.

    Forms inherit the page resources when they declare none, and a form shared
    across pages is visited once.
    """
    seen: set[tuple[int, int]] = set()
    for page in pdf.pages:
        resources = page.get("/Resources")
        yield "page", page, resources
        yield from _iter_forms(resources, seen)


def _iter_forms(
    resources: pikepdf.Object | None,
    seen: set[tuple[int, int]],
) -> Iterator[tuple[str, pikepdf.Object, pikepdf.Object | None]]:
    if resources is None or "/XObject" not in resources:
        return
    for xobject in resources["/XObject"].values():
        if str(xobject.get("/Subtype")) != "/Form":
            continue
        key = xobject.objgen
        if key in seen:
            continue
        seen.add(key)
        own_resources = xobject.get("/Resources") or resources
        yield "form", xobject, own_resources
        yield from _iter_forms(own_resources, seen)


def process(
    container: pikepdf.Object,
    fonts: Mapping[str, Font],
    replacements: Sequence[Replacement],
) -> tuple[list, dict[str, int], bool]:
    """Rewrite one content stream, returning its instructions, hit counts, and whether it changed."""
    counts = {rep.search: 0 for rep in replacements}
    instructions = list(parse_content_stream(container))
    raw_font = Font.raw()
    current_font = raw_font
    segment: list[_Chunk] = []
    changes: dict[tuple[int, int | None], bytes] = {}

    def flush() -> None:
        if segment:
            _apply_segment(segment, replacements, counts, changes)
            segment.clear()

    for index, instruction in enumerate(instructions):
        if not isinstance(instruction, ContentStreamInstruction):
            continue  # inline image or other non-show op — segment continues across it

        operator = str(instruction.operator)
        operands = instruction.operands

        if operator == "Tf":
            flush()
            if len(operands) >= _TF_MIN_OPERANDS:
                current_font = fonts.get(str(operands[-2]), raw_font)
        elif operator in ("BT", "ET"):
            flush()
        elif operator in _SHOW_SINGLE and operands and isinstance(operands[-1], pikepdf.String):
            text, slots = current_font.decode(bytes(operands[-1]))
            segment.append(_Chunk(current_font, text, slots, new_op=True, instr_index=index, element_index=None))
        elif operator == "TJ" and operands and isinstance(operands[-1], pikepdf.Array):
            _add_array_chunks(segment, current_font, operands[-1], index)

    flush()
    return _rebuild(instructions, changes), counts, bool(changes)


def _add_array_chunks(segment: list[_Chunk], font: Font, array: pikepdf.Array, instr_index: int) -> None:
    """Add one chunk per string element of a TJ array; only the first is an operator boundary."""
    first = True
    for element_index, element in enumerate(array):
        if not isinstance(element, pikepdf.String):
            continue
        text, slots = font.decode(bytes(element))
        segment.append(_Chunk(font, text, slots, new_op=first, instr_index=instr_index, element_index=element_index))
        first = False


def _apply_segment(
    segment: Sequence[_Chunk],
    replacements: Sequence[Replacement],
    counts: dict[str, int],
    changes: dict[tuple[int, int | None], bytes],
) -> None:
    """Match against the segment's reconstructed text and record the rewritten pieces."""
    full, boundaries = _reconstruct(segment)
    matches = _find_matches(full, boundaries, replacements, counts)
    if not matches:
        return

    offset = 0
    match_cursor = 0
    for chunk in segment:
        start, end = offset, offset + len(chunk.text)
        offset = end
        if not chunk.text:
            continue
        new_text, changed, match_cursor = _rewrite_chunk(full, matches, start, end, match_cursor)
        if not changed:
            continue
        preserve_slots = len(new_text) == len(chunk.text)
        key = (chunk.instr_index, chunk.element_index)
        changes[key] = chunk.font.encode(new_text, chunk.slots, preserve_slots=preserve_slots)


def _reconstruct(segment: Sequence[_Chunk]) -> tuple[str, set[int]]:
    """Join the segment's text and mark the offsets where a new operator begins."""
    parts: list[str] = []
    boundaries: set[int] = set()
    length = 0
    for position, chunk in enumerate(segment):
        if position > 0 and chunk.new_op:
            boundaries.add(length)
        parts.append(chunk.text)
        length += len(chunk.text)
    return "".join(parts), boundaries


def _find_matches(
    full: str,
    boundaries: set[int],
    replacements: Sequence[Replacement],
    counts: dict[str, int],
) -> list[tuple[int, int, Replacement]]:
    """Scan left to right for non-overlapping matches, trying replacements in priority order."""
    matches: list[tuple[int, int, Replacement]] = []
    index = 0
    while index < len(full):
        hit = None
        end = index
        for rep in replacements:
            if not rep.search:
                continue
            candidate = match_flex_at(full, boundaries, index, rep.search)
            if candidate > index:
                hit, end = rep, candidate
                break
        if hit is None:
            index += 1
            continue
        matches.append((index, end, hit))
        counts[hit.search] += 1
        index = end
    return matches


def _rewrite_chunk(
    full: str,
    matches: Sequence[tuple[int, int, Replacement]],
    start: int,
    end: int,
    match_cursor: int,
) -> tuple[str, bool, int]:
    """Produce a chunk's replacement text, walking the shared match cursor forward."""
    out: list[str] = []
    changed = False
    position = start
    while position < end:
        while match_cursor < len(matches) and matches[match_cursor][1] <= position:
            match_cursor += 1
        char = full[position]
        if match_cursor >= len(matches) or position < matches[match_cursor][0]:
            out.append(char)
        else:
            match_start, _, rep = matches[match_cursor]
            changed = True
            if rep.mask_char is not None:
                out.append(rep.mask_char)
            elif position == match_start:
                out.append(rep.replace)
        position += 1
    return "".join(out), changed, match_cursor


def _rebuild(instructions: Sequence, changes: Mapping[tuple[int, int | None], bytes]) -> list:
    """Rebuild only the instructions whose bytes changed, leaving the rest untouched."""
    if not changes:
        return list(instructions)
    per_instruction: dict[int, dict[int | None, bytes]] = defaultdict(dict)
    for (instr_index, element_index), data in changes.items():
        per_instruction[instr_index][element_index] = data

    result = list(instructions)
    for instr_index, edits in per_instruction.items():
        instruction = instructions[instr_index]
        operands = list(instruction.operands)
        if None in edits:
            operands[-1] = pikepdf.String(edits[None])
        else:
            elements = list(operands[-1])
            for element_index, data in edits.items():
                elements[element_index] = pikepdf.String(data)
            operands[-1] = pikepdf.Array(elements)
        result[instr_index] = ContentStreamInstruction(operands, instruction.operator)
    return result


def write_back(pdf: pikepdf.Pdf, kind: str, container: pikepdf.Object, instructions: list) -> None:
    """Store rewritten instructions back onto a page or Form XObject."""
    new_bytes = pikepdf.unparse_content_stream(instructions)
    if kind == "page":
        container.Contents = pdf.make_stream(new_bytes)
    else:
        container.write(new_bytes)
