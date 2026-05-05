/// Basic Dart constructs.
library comprehensive;

String greet(String name) => 'Hello, $name!';

int add(int a, int b) => a + b;

double? divide(double a, double b) {
  if (b == 0) return null;
  return a / b;
}

List<T> transform<T>(List<T> items, T Function(T) fn) {
  return items.map(fn).toList();
}

const String version = '1.0.0';

class Config {
  final String env;
  final bool debug;

  Config({this.env = 'development', this.debug = false});

  bool get isProduction => env == 'production';
}
