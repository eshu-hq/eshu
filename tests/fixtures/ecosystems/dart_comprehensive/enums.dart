/// Enum patterns.

enum Color {
  red(0xFF0000),
  green(0x00FF00),
  blue(0x0000FF);

  final int value;
  const Color(this.value);

  String get hex => '#${value.toRadixString(16).padLeft(6, '0')}';
}

enum Status {
  active,
  inactive,
  pending,
  deleted;

  bool get isTerminal => this == deleted;
}

sealed class Result<T> {
  const Result();
}

class Success<T> extends Result<T> {
  final T value;
  const Success(this.value);
}

class Failure<T> extends Result<T> {
  final String error;
  const Failure(this.error);
}
