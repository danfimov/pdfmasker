import pytest

from pdfmasker.backends.pikepdf.fold import match_flex_at


@pytest.mark.parametrize(
    ("full", "boundaries", "start", "pattern", "end"),
    [
        ("HELLO", set(), 0, "HELL", 4),  # prefix match
        ("hello", set(), 0, "HELLO", 5),  # case-insensitive
        ("xxAB", set(), 2, "AB", 4),  # match at an offset
        ("hello", set(), 0, "xyz", -1),  # no match
        ("A B", set(), 0, "A B", 3),  # space matches a real space glyph
        ("A  B", set(), 0, "A B", 4),  # a run of whitespace is one flexible gap
        ("A   B", set(), 0, "A   B", 5),  # collapsed pattern whitespace
        ("café CAFÉ", set(), 0, "café café", 9),  # unicode fold across a space
    ],
)
def test_match_flex_at(full, boundaries, start, pattern, end):
    assert match_flex_at(full, boundaries, start, pattern) == end


def test_space_matches_an_operator_boundary():
    # "AB" with an operator boundary between the two letters stands in for "A B".
    assert match_flex_at("AB", {1}, 0, "A B") == 2


def test_space_without_whitespace_or_boundary_does_not_match():
    # "income" has neither whitespace nor a boundary between "in" and "come".
    assert match_flex_at("income", set(), 0, "in come") == -1
