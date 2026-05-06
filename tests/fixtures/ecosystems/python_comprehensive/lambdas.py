"""Lambda patterns for parser testing."""

from typing import Callable

# Lambda assignments
double = lambda x: x * 2
add = lambda x, y: x + y
identity = lambda x: x

# Lambdas in data structures
OPERATIONS = {
    "add": lambda a, b: a + b,
    "sub": lambda a, b: a - b,
    "mul": lambda a, b: a * b,
}


def apply_transform(items: list, transform: Callable = lambda x: x) -> list:
    """Function with lambda default argument."""
    return [transform(item) for item in items]


def create_multiplier(factor: int) -> Callable[[int], int]:
    """Returns a lambda closure."""
    return lambda x: x * factor


class Sorter:
    """Class using lambdas."""

    def sort_by_key(self, items: list[dict], key: str) -> list[dict]:
        return sorted(items, key=lambda item: item.get(key, ""))

    def filter_items(self, items: list, predicate: Callable = lambda x: True) -> list:
        return [item for item in items if predicate(item)]
