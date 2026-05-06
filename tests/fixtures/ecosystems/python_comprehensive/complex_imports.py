"""Complex import patterns for parser testing."""

import os
import sys as system
from pathlib import Path
from typing import (
    Optional,
    List,
    Dict,
    Union,
    Any,
)
from collections import defaultdict, OrderedDict
from os.path import join as path_join, exists as path_exists
from . import basic
from .decorators import timer, retry
from .inheritance import Animal, Dog as DogClass


__all__ = ["process_data", "DataProcessor"]

SENTINEL = object()


def process_data(items: List[Any]) -> Dict[str, Any]:
    """Function using various imports."""
    result: Dict[str, Any] = defaultdict(list)
    base = Path(system.prefix)
    if path_exists(str(base)):
        result["base"] = str(base)
    return dict(result)


class DataProcessor:
    """Class using imported types."""

    def __init__(self, config: Optional[dict] = None):
        self.config = config or {}
        self.data: OrderedDict = OrderedDict()

    @timer
    def run(self, items: List[Union[str, int]]) -> Dict[str, Any]:
        return process_data(items)
