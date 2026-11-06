#!/usr/bin/env python3
"""Select the go test event lines the fake go of the readiness self-test emits.

The self-test puts a fake ``go`` on PATH. It replays a recorded
``go test -json`` stream, but only the events a real ``go test -run PATTERN``
would produce: tests the pattern selects (plus their subtests), tests outside
the enrolled set, and every line that names no test. This helper does that
selection in one process. The fake used to run the pattern through a bash
``[[ =~ ]]`` once per name and once per event line; bash recompiles the
pattern on every match, and the largest package's pattern is about 28 KB over
533 names, so one runner invocation spent about 19 s there (#10238).

Usage:
    live_postgres_readiness_fake_go_events.py PATTERN NAMES EVENTS

Exit status: 0 with the selected lines on stdout; 9 with a message on
stderr when the pattern selects none of the enrolled names, when it also
selects the decoy name TestZzzDecoyNotEnrolled (it is not anchored), or when
it is not a valid regular expression. 9 is the status the fake go has always
used for a rejected call.
"""

from __future__ import annotations

import re
import sys

# DECOY is a name that is never enrolled. A pattern that selects it is not
# anchored, so it would also select tests the proof list does not name.
DECOY = "TestZzzDecoyNotEnrolled"
# TEST_KEY precedes the test name in a go test -json event line.
TEST_KEY = b'"Test":"'
REJECTED = 9


def read_lines(path: str) -> list[bytes]:
    """Return the complete lines of a file the way a bash read -r loop does.

    A final line with no newline is dropped, as ``read`` returns nonzero for
    it and the loop body never runs.
    """
    with open(path, "rb") as handle:
        data = handle.read()
    lines = data.split(b"\n")
    lines.pop()
    return lines


def event_test(line: bytes) -> bytes | None:
    """Return the test name an event line carries, or None when it has none."""
    start = line.find(TEST_KEY)
    if start < 0:
        return None
    rest = line[start + len(TEST_KEY):]
    return rest.split(b'"', 1)[0]


def select(pattern: re.Pattern[bytes], names: set[bytes], lines: list[bytes]) -> list[bytes]:
    """Return the lines a go test run for the pattern would have emitted."""
    kept: list[bytes] = []
    for line in lines:
        test = event_test(line)
        if (test is None or pattern.search(test)
                or (b"/" in test and pattern.search(test.split(b"/", 1)[0]))
                or test not in names):
            kept.append(line)
    return kept


def main(argv: list[str]) -> int:
    """Check the pattern, select the event lines, and print them."""
    if len(argv) != 3:
        print("usage: PATTERN NAMES EVENTS", file=sys.stderr)
        return 2
    try:
        pattern = re.compile(argv[0].encode())
    except re.error as error:
        print(f"fake go: -run pattern is not a regular expression: {error}", file=sys.stderr)
        return REJECTED
    names = set(read_lines(argv[1]))
    if not any(pattern.search(name) for name in names):
        print("pattern matched nothing", file=sys.stderr)
        return REJECTED
    if pattern.search(DECOY.encode()):
        print("pattern is not anchored", file=sys.stderr)
        return REJECTED
    selected = select(pattern, names, read_lines(argv[2]))
    sys.stdout.buffer.write(b"".join(line + b"\n" for line in selected))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
