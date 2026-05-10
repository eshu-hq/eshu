# TDD Workflow

Use this file when planning or executing a Go change. TDD is the default, not the fallback.

## Standard Loop

1. Define the contract.
   State expected behavior, edge cases, and failure mode before editing code.
2. Write the smallest failing test.
   Match the repository's existing test style and location.
3. Confirm the test fails for the expected reason.
   If it passes, the test does not prove the bug or missing behavior.
4. Implement the smallest change to make it pass.
5. Re-run the focused test scope.
6. Refactor with tests green.
7. Run broader checks only after the focused proof is solid.

## Bug Fix Workflow

1. Reproduce the bug in a regression test.
2. Watch the regression fail before the fix.
3. Implement the fix.
4. Re-run the regression and nearby affected tests.
5. Update docs if the fix changes observable behavior or a public contract.

## Test Design Rules

- Test behavior, contracts, and edge cases.
- Avoid asserting implementation details when observable behavior proves the point.
- Prefer direct tests of concrete types over mocks unless the boundary is real and expensive to cross.
- Keep fixtures small and obvious.
- Use table-driven tests when multiple cases share setup and the table improves readability.
- Use subtests when names help isolate and explain cases.
- Use `t.Helper()` in test helpers that would otherwise hide the real failure location.
- Use `t.TempDir()` and `t.Setenv()` instead of hand-managed cleanup where possible.
- Add `t.Parallel()` only when the test is actually safe to parallelize.

## When the Repo Is Silent

- Put tests next to the code in `_test.go` files unless the repo has a strong external test-package pattern.
- Start with package-level unit tests.
- Use integration tests only when the contract crosses process, database, filesystem, or network boundaries.
- Use end-to-end tests only when lower layers cannot prove the behavior.
- Prefer table-driven tests for input-output style logic.
- Do not force table-driven tests when one or two direct tests are clearer.

## Failure-First Example

Write the failing test first:

```go
func TestParsePortRejectsEmptyInput(t *testing.T) {
	t.Parallel()

	_, err := ParsePort("")
	if err == nil {
		t.Fatal("ParsePort(\"\") error = nil, want non-nil")
	}
}
```

Then implement the minimal behavior:

```go
func ParsePort(raw string) (int, error) {
	if raw == "" {
		return 0, errors.New("port is empty")
	}

	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse port %q: %w", raw, err)
	}

	return port, nil
}
```

Bad order:

```go
func ParsePort(raw string) (int, error) {
	// implementation first
}

func TestParsePortRejectsEmptyInput(t *testing.T) {
	// test written after the code already works
}
```

That proves only that the final code passes the test. It does not prove the test caught the missing behavior.

## Verification Minimum

Before claiming completion:
- run the focused failing-now-passing test
- run nearby affected package tests
- run broader `go test` coverage when the change crosses package boundaries
- run formatting and linting required by the repo
- report any verification gaps explicitly
