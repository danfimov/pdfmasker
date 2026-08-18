import pytest

from pdfmasker.backends.pikepdf.fonts import Font


def cid_font(forward):
    reverse = {}
    for code, text in forward.items():
        if text and text[0] not in reverse:
            reverse[text[0]] = code
    return Font(is_cid=True, forward=forward, reverse=reverse)


@pytest.mark.parametrize(
    ("font", "data", "expected"),
    [
        (cid_font({0x41: "A", 0x42: "B"}), b"\x00\x41\x00\x42", ("AB", 2)),
        (cid_font({0x41: "A"}), b"\x00\x41\x00\x99", ("A", 2)),  # unmapped code still counts a slot
        (cid_font({0x41: "A"}), b"\x00\x41\x00", ("A", 1)),  # odd trailing byte dropped
        (Font.raw(), b"ABC", ("ABC", 3)),  # no map -> latin1
    ],
)
def test_decode(font, data, expected):
    assert font.decode(data) == expected


@pytest.mark.parametrize(
    ("font", "text", "slots", "expected"),
    [
        (cid_font({0x58: "X", 0x41: "A"}), "XA", 2, b"\x00\x58\x00\x41"),  # exact
        (cid_font({0x58: "X"}), "X", 3, b"\x00\x58\x00\x58\x00\x58"),  # pad with fallback glyph
        (cid_font({0x58: "X", 0x41: "A"}), "XA", 1, b"\x00\x58"),  # truncate overflow
        (Font.raw(), "####", 7, b"####\x00\x00\x00"),  # simple pad with zero byte
        (Font.raw(), "ABCDE", 3, b"ABC"),  # simple truncate
    ],
)
def test_encode_preserving_slots(font, text, slots, expected):
    assert font.encode(text, slots, preserve_slots=True) == expected


@pytest.mark.parametrize(
    ("font", "text", "expected"),
    [
        (cid_font({0x58: "X", 0x41: "A"}), "XAX", b"\x00\x58\x00\x41\x00\x58"),  # natural CID length
        (Font.raw(), "MINERVA", b"MINERVA"),  # natural simple length, no padding
    ],
)
def test_encode_natural_length(font, text, expected):
    assert font.encode(text, slots=1, preserve_slots=False) == expected


@pytest.mark.parametrize(
    ("font", "expected"),
    [
        (cid_font({0x2D: "-", 0x30: "0"}), b"\x00\x2d"),  # "-" precedes "0" in preference
        (cid_font({0x99: "q"}), b"\x00\x99"),  # no preferred glyph -> any
        (Font(is_cid=True, forward={}, reverse={}), b"\x00\x00"),  # nothing -> zero
    ],
)
def test_padding_glyph_selection(font, expected):
    # Padding an empty string reveals which fallback glyph fills the slot.
    assert font.encode("", 1, preserve_slots=True) == expected


@pytest.mark.parametrize("slots", [0, 1, 4, 9])
def test_cid_encode_always_returns_exact_byte_length(slots):
    font = cid_font({0x58: "X"})
    assert len(font.encode("XXXX", slots, preserve_slots=True)) == 2 * slots
