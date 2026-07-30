# Laravel Class@method Route Resolution Evidence (#5513)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Laravel `Class@method` Route Resolution (#5513)

No-Regression Evidence: Laravel string-callable route handlers now add a bounded
set of exact candidates during `handles_route`/`runs_in` intent extraction:
`Class@method` adds `Class.method`. Fully-qualified controller tokens remain
unresolved because the PHP parser currently exposes only a file-level namespace
and a valid PHP file can contain multiple namespace blocks; shortening an FQN
would fabricate cross-namespace truth. The baseline for non-Laravel frameworks
is byte-identical (one handler candidate and the same path-first,
repository-second exact map lookups). A short Laravel `@` token adds exactly one
dotted candidate. There is no bare-method or FQN-to-short-class fallback, no
graph or Postgres read, and the existing uniqueness fence remains in force, so
a wrong or ambiguous controller emits no edge. Tests cover both the same-file
lookup and the conventional cross-file Laravel layout, where `routes/routes.php`
resolves `app/Http/Controllers/UserController.php` through the repository-unique
candidate map; the B-7 fixture uses that cross-file layout too. Focused proof:
`go test ./internal/reducer -run 'TestBuild(HandlesRoute|RunsIn)IntentRows(EmitsPHPLaravel|DoesNot(ResolvePHPLaravelNamespacedAtJoinedRoute|ResolveLaravelControllerFQN|ShortenLaravelControllerNamespace|BareMatchWrongLaravelController))' -count=1`
and `go test ./internal/query -run '^TestRouteQueryProofMatrix$/^(php_laravel|php_symfony)$' -count=1`.

No-Observability-Change: the change only selects an existing exact Function
candidate before the existing shared-projection intent is built. It adds no
route, graph query or write shape, queue domain, worker, lease, runtime knob,
metric instrument, metric label, span, status field, or log key. Operators keep
diagnosing the path through existing reducer execution counters/spans,
`handles_route` shared-intent status, and `HANDLES_ROUTE` graph/query truth.
