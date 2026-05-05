"""Nested class patterns for parser testing."""


class Outer:
    """Class with inner classes."""

    class Inner:
        """Nested inner class."""

        def inner_method(self) -> str:
            return "inner"

    class AnotherInner:
        """Another nested class."""

        class DeepNested:
            """Deeply nested class."""

            def deep_method(self) -> str:
                return "deep"

    def create_inner(self) -> "Outer.Inner":
        return Outer.Inner()


class MetaLogger(type):
    """Metaclass example."""

    def __new__(mcs, name, bases, namespace):
        cls = super().__new__(mcs, name, bases, namespace)
        print(f"Created class: {name}")
        return cls


class Logged(metaclass=MetaLogger):
    """Class using metaclass."""

    def do_something(self) -> None:
        pass
