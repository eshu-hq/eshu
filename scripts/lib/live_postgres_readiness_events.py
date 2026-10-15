"""Verify go-test pass events for the PostgreSQL readiness runner."""

from collections import deque
import json
import pathlib
import sys


def verify_results(
    events_path: pathlib.Path,
    package: str,
    packages: dict[str, dict[str, tuple[str, ...]]],
    skipped: set[str] | None = None,
    expected: list[str] | None = None,
) -> int:
    """Require one run and one passing terminal event per expected test of a package.

    Names in `skipped` are excused from the pass requirement: absence or a
    skip passes, but a failure still fails closed. When `expected` names an
    explicit subset (one readiness shard), only that subset may appear: a
    run or terminal event for any other test fails closed, so a shard whose
    -run pattern drifted wider than its assignment cannot pass. Subtests
    resolve to their top-level parent: `go test -run` matches the parent and
    emits Parent/child events, which are accepted exactly as the full suite
    accepts them (unattributed) when the parent is expected or excused.
    """
    excused = set(skipped or ())
    full_tests = {test for tests in packages[package].values() for test in tests}
    if expected is None:
        expected_tests = full_tests - excused
        strict = False
    else:
        unknown = sorted(set(expected) - full_tests)
        if unknown:
            print(f"unknown expected test(s) for {package}: {', '.join(unknown)}")
            return 1
        expected_tests = set(expected) - excused
        strict = True
    runs = {name: 0 for name in expected_tests}
    terminals: dict[str, list[tuple[str, float]]] = {
        name: [] for name in expected_tests
    }
    excused_terminals: dict[str, list[str]] = {name: [] for name in excused}
    package_terminal = []
    failed = False
    # Tail ring per test: a failing test's assertion is in its LAST output
    # events, so keep the trailing window, not the leading one (#7814).
    output: dict[str, deque[str]] = {
        name: deque(maxlen=12) for name in expected_tests
    }
    with events_path.open(encoding="utf-8") as events:
        for line_number, line in enumerate(events, start=1):
            try:
                event = json.loads(line)
            except json.JSONDecodeError as error:
                print(f"invalid go test JSON line {line_number}: {error}", file=sys.stderr)
                return 1
            action = event.get("Action")
            name = event.get("Test")
            if name in expected_tests:
                if action == "run":
                    runs[name] += 1
                elif action in {"pass", "fail", "skip"}:
                    terminals[name].append((action, event.get("Elapsed", 0)))
                elif action == "output":
                    output[name].append(event.get("Output", "").rstrip()[:300])
            elif name in excused_terminals and action in {"pass", "fail", "skip"}:
                excused_terminals[name].append(action)
            elif strict and name:
                parent = name.split("/")[0]
                if parent not in expected_tests and parent not in excused_terminals:
                    if action in {"run", "pass", "fail", "skip"}:
                        print(f"{name}: unexpected test event outside the shard subset")
                        failed = True
                    continue
                if "/" in name:
                    continue
            elif not name and action in {"pass", "fail", "skip"}:
                package_terminal.append(action)

    for name in sorted(expected_tests):
        terminal = terminals[name]
        if runs[name] != 1 or len(terminal) != 1:
            print(
                f"{name}: missing or duplicate event "
                f"(run={runs[name]}, terminal={len(terminal)})"
            )
            failed = True
            continue
        action, elapsed = terminal[0]
        print(f"{name}: {action.upper()} elapsed={elapsed:.3f}s")
        if action != "pass":
            failed = True
            for text in list(output[name])[-6:]:
                print(f"  {text}")
    for name in sorted(excused_terminals):
        if "fail" in excused_terminals[name]:
            print(f"{name}: FAIL (a --skip excuse does not cover failure)")
            failed = True
    if package_terminal != ["pass"]:
        print(f"package terminal ({package}): expected PASS, actual={package_terminal}")
        failed = True
    if failed:
        return 1
    print(
        f"live-postgres-readiness: {package} "
        f"{len(expected_tests)}/{len(expected_tests)} PASS"
    )
    return 0
