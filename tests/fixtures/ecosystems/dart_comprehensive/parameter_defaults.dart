class SizedBox { const SizedBox(); }
class Tag { const Tag(this.label); final String label; }
int compute() => 7;
void positionalDefault([int x = compute()]) {}
void namedDefault({SizedBox box = const SizedBox()}) {}
void annotatedParam(@Tag('p') int value) {}
class Service {
  Service({int retries = compute()});
  void run([int depth = compute()]) {}
}
