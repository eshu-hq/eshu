"""Inheritance patterns for parser testing."""

from abc import ABC, abstractmethod


class Animal(ABC):
    """Abstract base class for animals."""

    def __init__(self, name: str):
        self.name = name

    @abstractmethod
    def speak(self) -> str:
        ...

    def describe(self) -> str:
        return f"{self.name} says {self.speak()}"


class Dog(Animal):
    """Dog inherits from Animal."""

    def speak(self) -> str:
        return "Woof!"

    def fetch(self, item: str) -> str:
        return f"{self.name} fetched {item}"


class Cat(Animal):
    def speak(self) -> str:
        return "Meow!"


class LogMixin:
    """Mixin for logging."""

    def log(self, message: str) -> None:
        print(f"[{self.__class__.__name__}] {message}")


class SerializeMixin:
    """Mixin for serialization."""

    def to_dict(self) -> dict:
        return {"class": self.__class__.__name__}


class ServiceDog(Dog, LogMixin, SerializeMixin):
    """Multiple inheritance: Dog + LogMixin + SerializeMixin."""

    def __init__(self, name: str, job: str):
        super().__init__(name)
        self.job = job

    def work(self) -> str:
        self.log(f"Working as {self.job}")
        return f"{self.name} is working"


class GuideDog(ServiceDog):
    """Deep inheritance chain."""

    def guide(self, destination: str) -> str:
        return f"Guiding to {destination}"
