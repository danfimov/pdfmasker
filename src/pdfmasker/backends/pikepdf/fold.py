"""Whitespace-flexible, case-insensitive matching over reconstructed segment text.

Text is reassembled from consecutive show-text operators, so the visual space
between two words is sometimes a real space glyph and sometimes just a positioning
jump between operators. A space in the target therefore matches either real
whitespace or a boundary between operators, which lets a full name match across the
operators that render it while still refusing to match inside a single word.
"""


def _fold(char: str) -> str:
    return char.casefold()


def match_flex_at(full: str, boundaries: set[int], start: int, pattern: str) -> int:
    """Return the end index of a match beginning at a given offset, or -1.

    Whitespace in the pattern collapses to one flexible gap that matches either a
    run of whitespace or a zero-width operator boundary; every other character must
    match under case folding.
    """
    text_index = start
    pattern_index = 0
    length = len(pattern)

    while pattern_index < length:
        if pattern[pattern_index].isspace():
            while pattern_index < length and pattern[pattern_index].isspace():
                pattern_index += 1
            consumed = 0
            while text_index < len(full) and full[text_index].isspace():
                text_index += 1
                consumed += 1
            if consumed == 0 and text_index not in boundaries:
                return -1
            continue

        if text_index >= len(full) or _fold(full[text_index]) != _fold(pattern[pattern_index]):
            return -1
        text_index += 1
        pattern_index += 1

    return text_index
