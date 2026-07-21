/// Call-graph shapes for the golden-corpus gate (eshu-hq/eshu#5349).
///
/// Every shape below mirrors a committed regression test in
/// go/internal/parser/dart/calls_ast_test.go and calls_shapes_test.go — the
/// eshu-hq/eshu#5332 fix for the declaration-vs-call-site self-loop bug (a
/// pre-fix raw byte-scanner recorded every function/method/constructor
/// declaration as a spurious call to itself). Staging this fixture in the
/// golden 20-repo corpus (scripts/verify-golden-corpus-gate.sh) and pinning
/// its exact CALLS self-loop count in the B-12 snapshot
/// (testdata/golden/e2e-20repo-snapshot.json, sl-dart-calls-recursion) is what
/// #5332 shipped without: Dart was not in the corpus, so the corruption class
/// had no durable end-to-end gate.

// Declaration-only class: zero call sites. A pre-#5332 scanner recorded every
// declaration below (two constructors, a getter, a setter, a method) as a
// spurious self-call; the AST-based extractor must emit zero function_calls
// rows for this class.
class DeclarationOnlyWidget {
  DeclarationOnlyWidget();
  DeclarationOnlyWidget.named();
  int get value => 0;
  set value(int v) {}
  void run() {}
}

// Arrow-form recursion: a genuine self-referential CALLS edge (caller ==
// callee) on the same line as its own declaration. Must survive — this is one
// of the exactly 2 self-loops pinned by sl-dart-calls-recursion.
int recursionFib(int n) =>
    n < 2 ? n : recursionFib(n - 1) + recursionFib(n - 2);

// Block-form recursion: the call site is on a different line than the
// declaration. Must survive as a self-loop CALLS edge, not be conflated with
// the (correctly call-free) declaration line — the other of the 2 pinned
// self-loops.
int recursionFact(int n) {
  if (n <= 1) {
    return 1;
  }
  return n * recursionFact(n - 1);
}

// Mutual recursion: both directed edges (mutualPing -> mutualPong and
// mutualPong -> mutualPing) must survive as distinct CALLS rows. Neither is a
// self-loop (caller != callee), so this does NOT change the pinned self-loop
// count above; it proves the fix does not overcorrect into dropping
// cross-function recursion while suppressing self-loops.
void mutualPing() {
  mutualPong();
}

void mutualPong() {
  mutualPing();
}

// Named-constructor call: the call site (`GeoPoint.origin()`) must produce a
// row keyed off the constructor name; the constructor's own declaration (a
// disjoint constructor_signature grammar node, never an argument list) must
// not produce a row.
class GeoPoint {
  final int x;
  GeoPoint.origin() : x = 0;
}

void useNamedConstructor() {
  GeoPoint.origin();
}

// Generic-type-argument call: the type_arguments selector between the callee
// and the argument_part selector must not break the call chain or drop the
// call.
T identityFn<T>(T value) => value;

void useGenericArgsCall() {
  identityFn<int>(5);
}

// Cascade: repeat cascade calls to the same method dedup by full_name into one
// function_calls row (documented row-volume-collapse behavior in
// calls_shapes_test.go), keyed to the first cascade call's line.
class LineBuffer {
  void append(String s) {}
}

void useCascade(LineBuffer buf) {
  buf
    ..append('a')
    ..append('b');
}
