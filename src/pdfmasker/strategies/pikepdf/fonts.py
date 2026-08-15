"""Round-trip show-text bytes through a font while keeping the layout stable.

Replacements are re-encoded into the same number of glyph slots as the original
token so glyph positions and downstream offsets never shift. Type0 fonts use
two-byte codes resolved through the ToUnicode map; simple fonts use one byte,
falling back to Latin-1 when no map is embedded.
"""

import pikepdf

from pdfmasker.strategies.pikepdf.cmap import parse_tounicode

# Padding glyphs to try, in order, when a replacement is shorter than the slot it
# must fill. These are common enough to exist in most subset fonts.
_FALLBACK_PREFERENCE = "Xx*#-_.0"

_CID_BYTES_PER_SLOT = 2


class Font:
    """A font viewed through its ToUnicode map, able to decode and re-encode text."""

    def __init__(self, *, is_cid: bool, forward: dict[int, str], reverse: dict[str, int]) -> None:
        """Hold the font kind and its ToUnicode lookup tables."""
        self.is_cid = is_cid
        self.forward = forward
        self.reverse = reverse

    @classmethod
    def from_dict(cls, font_dict: pikepdf.Object) -> "Font":
        """Build a font from its PDF dictionary, reading the ToUnicode map if present."""
        is_cid = str(font_dict.get("/Subtype")) == "/Type0"
        forward: dict[int, str] = {}
        reverse: dict[str, int] = {}
        if "/ToUnicode" in font_dict:
            forward, reverse = parse_tounicode(bytes(font_dict["/ToUnicode"].read_bytes()))
        return cls(is_cid=is_cid, forward=forward, reverse=reverse)

    @classmethod
    def raw(cls) -> "Font":
        """Build a mapping-less font for show-text that has no resolvable current font.

        Bytes pass through as Latin-1 so such tokens still round-trip safely.
        """
        return cls(is_cid=False, forward={}, reverse={})

    def decode(self, data: bytes) -> tuple[str, int]:
        """Decode show-text bytes into their text and the number of glyph slots.

        A code without a ToUnicode entry yields no character but still counts as a
        slot, so an unmapped glyph is preserved rather than dropped on re-encode.
        """
        if self.is_cid:
            slots = len(data) // _CID_BYTES_PER_SLOT
            chars = []
            for slot in range(slots):
                code = (data[2 * slot] << 8) | data[2 * slot + 1]
                mapped = self.forward.get(code, "")
                chars.append(mapped[0] if mapped else "")
            return "".join(chars), slots
        if self.forward:
            chars = [(self.forward.get(byte, "") or "")[:1] for byte in data]
            return "".join(chars), len(data)
        return data.decode("latin1"), len(data)

    def _fallback_code(self) -> int:
        for char in _FALLBACK_PREFERENCE:
            if char in self.reverse:
                return self.reverse[char]
        return next(iter(self.reverse.values()), 0)

    def encode(self, text: str, slots: int, *, preserve_slots: bool) -> bytes:
        """Encode text back into font bytes.

        With slot preservation the result keeps the original token's width so the
        visual layout stays stable; otherwise the text is encoded at its natural
        length, which a length-changing custom mask needs.
        """
        if preserve_slots:
            return self._encode_slots(text, slots)
        if self.is_cid:
            fallback = self._fallback_code()
            return b"".join(self._cid_bytes(self.reverse.get(char, fallback)) for char in text)
        return text.encode("latin1", "replace")

    def _encode_slots(self, text: str, slots: int) -> bytes:
        if self.is_cid:
            fallback = self._fallback_code()
            out = bytearray()
            for index in range(slots):
                code = self.reverse.get(text[index], fallback) if index < len(text) else fallback
                out += self._cid_bytes(code)
            return bytes(out)
        raw = text.encode("latin1", "replace")[:slots]
        return raw + b"\x00" * (slots - len(raw))

    @staticmethod
    def _cid_bytes(code: int) -> bytes:
        return bytes(((code >> 8) & 0xFF, code & 0xFF))


def collect_fonts(resources: pikepdf.Object | None) -> dict[str, Font]:
    """Map each font name in a resource dictionary to its decoded font."""
    fonts: dict[str, Font] = {}
    if resources is not None and "/Font" in resources:
        for name, font_dict in resources["/Font"].items():
            fonts[str(name)] = Font.from_dict(font_dict)
    return fonts
