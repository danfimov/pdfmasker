import pytest

from pdfmasker.strategies.pikepdf.cmap import parse_tounicode

BFCHAR = b"""
2 beginbfchar
<0041> <0061>
<0042> <0062>
endbfchar
"""

BFRANGE_BASE = b"""
1 beginbfrange
<0041> <0043> <0061>
endbfrange
"""

BFRANGE_ARRAY = b"""
1 beginbfrange
<0041> <0043> [<0078> <0079> <007a>]
endbfrange
"""

MULTI_UNIT = b"""
1 beginbfchar
<0001> <0066006600690066006c>
endbfchar
"""

FIRST_WINS = b"""
2 beginbfchar
<0005> <0041>
<0009> <0041>
endbfchar
"""


@pytest.mark.parametrize(
    ("cmap", "forward"),
    [
        (BFCHAR, {0x41: "a", 0x42: "b"}),
        (BFRANGE_BASE, {0x41: "a", 0x42: "b", 0x43: "c"}),
        (BFRANGE_ARRAY, {0x41: "x", 0x42: "y", 0x43: "z"}),
        (MULTI_UNIT, {0x01: "ffifl"}),
    ],
)
def test_forward_map(cmap, forward):
    assert parse_tounicode(cmap)[0] == forward


def test_reverse_map_is_first_wins():
    assert parse_tounicode(FIRST_WINS)[1] == {"A": 0x05}
