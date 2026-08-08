"""Masking strategies and the shared result/interface types."""

from pdfmasker.strategies.base import MaskResult, MaskStrategy
from pdfmasker.strategies.text_layer import TextLayerStrategy

__all__ = [
    "MaskResult",
    "MaskStrategy",
    "TextLayerStrategy",
]
