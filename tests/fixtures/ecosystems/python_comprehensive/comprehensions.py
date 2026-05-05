"""Comprehension patterns for parser testing."""


def list_comprehension_examples(data: list[int]) -> list[int]:
    """Various list comprehensions."""
    squares = [x ** 2 for x in data]
    evens = [x for x in data if x % 2 == 0]
    nested = [x * y for x in range(3) for y in range(3)]
    return squares + evens + nested


def dict_comprehension_examples(keys: list[str], values: list[int]) -> dict:
    """Dictionary comprehensions."""
    mapping = {k: v for k, v in zip(keys, values)}
    filtered = {k: v for k, v in mapping.items() if v > 0}
    return filtered


def set_comprehension_examples(data: list[int]) -> set[int]:
    """Set comprehensions."""
    unique_squares = {x ** 2 for x in data}
    return unique_squares


def generator_examples(n: int):
    """Generator expressions and functions."""
    total = sum(x ** 2 for x in range(n))

    def fibonacci():
        a, b = 0, 1
        while True:
            yield a
            a, b = b, a + b

    return total, fibonacci
