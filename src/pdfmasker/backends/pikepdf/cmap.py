"""Decode a font's embedded ToUnicode CMap into usable lookup tables.

pikepdf hands back the raw CMap stream but does not interpret it, so the glyph
codes shown in a content stream cannot be turned into text without this step. The
two constructs that carry the mapping are parsed: single `bfchar` entries and both
forms of `bfrange` (a contiguous base and an explicit destination array).
"""

import re

_BFCHAR_BLOCK = re.compile(rb"beginbfchar(.*?)endbfchar", re.DOTALL)
_BFRANGE_BLOCK = re.compile(rb"beginbfrange(.*?)endbfrange", re.DOTALL)
_HEX = re.compile(rb"<([0-9A-Fa-f]+)>")
_BFCHAR_PAIR = re.compile(rb"<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>")
# A bfrange entry ends with either a single base value or a bracketed array. The
# array branch is consumed whole so its inner hex values are not mistaken for a
# separate contiguous-range entry.
_BFRANGE_ENTRY = re.compile(
    rb"<([0-9A-Fa-f]+)>\s*<([0-9A-Fa-f]+)>\s*(?:<([0-9A-Fa-f]+)>|\[([^\]]*)\])",
    re.DOTALL,
)


def _hex_to_text(hex_bytes: bytes) -> str:
    raw = bytes.fromhex(hex_bytes.decode("ascii"))
    if len(raw) % 2:
        raw = b"\x00" + raw
    return raw.decode("utf-16-be", "replace")


def parse_tounicode(raw: bytes) -> tuple[dict[int, str], dict[str, int]]:
    """Turn a ToUnicode stream into forward and reverse lookup tables.

    The forward table maps a glyph code to its text; the reverse table maps a
    character back to a code and keeps the first code seen, so re-encoding is
    deterministic when several codes share a character.
    """
    forward: dict[int, str] = {}

    for block in _BFCHAR_BLOCK.findall(raw):
        for src, dst in _BFCHAR_PAIR.findall(block):
            forward[int(src, 16)] = _hex_to_text(dst)

    for block in _BFRANGE_BLOCK.findall(raw):
        for lo, hi, base, array in _BFRANGE_ENTRY.findall(block):
            lo_code = int(lo, 16)
            if base:
                base_code = int(base, 16)
                for offset in range(int(hi, 16) - lo_code + 1):
                    forward[lo_code + offset] = chr(base_code + offset)
            else:
                for offset, dst in enumerate(_HEX.findall(array)):
                    forward[lo_code + offset] = _hex_to_text(dst)

    reverse: dict[str, int] = {}
    for code, text in forward.items():
        if text and text[0] not in reverse:
            reverse[text[0]] = code
    return forward, reverse
