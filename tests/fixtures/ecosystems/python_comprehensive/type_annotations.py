"""Type annotation patterns for parser testing."""

from dataclasses import dataclass, field
from typing import TypeVar, Generic, Protocol, Literal, Union, Optional

T = TypeVar("T")
K = TypeVar("K")
V = TypeVar("V")

Status = Literal["active", "inactive", "pending"]


class Comparable(Protocol):
    """Protocol for comparable objects."""

    def __lt__(self, other: "Comparable") -> bool: ...
    def __eq__(self, other: object) -> bool: ...


@dataclass
class Point:
    """Dataclass with type annotations."""
    x: float
    y: float
    label: str = ""

    def distance_to(self, other: "Point") -> float:
        return ((self.x - other.x) ** 2 + (self.y - other.y) ** 2) ** 0.5


@dataclass
class Container(Generic[T]):
    """Generic dataclass."""
    items: list[T] = field(default_factory=list)

    def add(self, item: T) -> None:
        self.items.append(item)

    def get_first(self) -> Optional[T]:
        return self.items[0] if self.items else None


class Registry(Generic[K, V]):
    """Generic class with multiple type parameters."""

    def __init__(self) -> None:
        self._store: dict[K, V] = {}

    def register(self, key: K, value: V) -> None:
        self._store[key] = value

    def lookup(self, key: K) -> Optional[V]:
        return self._store.get(key)


def first_match(items: list[T], predicate: callable) -> Optional[T]:
    """Generic function."""
    for item in items:
        if predicate(item):
            return item
    return None
