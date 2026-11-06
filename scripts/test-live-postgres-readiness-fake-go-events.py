#!/usr/bin/env python3
"""Check the event selector behind the readiness self-test's fake go.

The fake go replays a recorded ``go test -json`` stream through
``live_postgres_readiness_fake_go_events.py``. The self-test's verdicts rest on
that selector, so it gets its own cases, and each seeded mutation of the
selector must fail them (#10238).
"""

from __future__ import annotations

import pathlib
import subprocess
import sys
import tempfile
import unittest

LIB = pathlib.Path(__file__).parent / "lib"
HELPER = LIB / "live_postgres_readiness_fake_go_events.py"

NAMES = ["TestAlpha", "TestBeta", "TestGamma"]
PATTERN = "^(TestAlpha|TestBeta|TestGamma)$"
RUN = '{"Action":"run","Test":"%s"}'
PASS = '{"Action":"pass","Test":"%s","Elapsed":0.25}'
TERMINAL = '{"Action":"pass","Package":"example.test/pkg","Elapsed":1.0}'


def select(helper: pathlib.Path, pattern: str, names: list[str], events: str):
    """Run a selector copy and return its status, stderr, and selected lines."""
    with tempfile.TemporaryDirectory() as temp:
        root = pathlib.Path(temp)
        (root / "names").write_text("".join(f"{n}\n" for n in names), encoding="utf-8")
        (root / "events").write_text(events, encoding="utf-8")
        done = subprocess.run(
            [sys.executable, str(helper), pattern, str(root / "names"),
             str(root / "events")],
            capture_output=True, text=True, check=False)
        return done.returncode, done.stderr, done.stdout.splitlines()


def stream(*lines: str, last_newline: bool = True) -> str:
    """Join event lines into a recorded stream."""
    return "\n".join(lines) + ("\n" if last_newline else "")


def problems(helper: pathlib.Path) -> list[str]:
    """Return every selector rule the helper copy breaks."""
    found: list[str] = []

    def expect(label: str, got, want) -> None:
        if got != want:
            found.append(f"{label}: got {got!r}, want {want!r}")

    clean = stream(*(f % n for n in NAMES for f in (RUN, PASS)), TERMINAL)
    status, _, lines = select(helper, PATTERN, NAMES, clean)
    expect("clean stream", (status, len(lines)), (0, 7))
    # A line naming no test is kept (the package terminal, blanks, noise).
    _, _, lines = select(helper, PATTERN, NAMES, stream(TERMINAL, "", "plain text"))
    expect("lines with no test name", lines, [TERMINAL, "", "plain text"])
    # A test outside the enrolled set is kept: the pattern never selected it,
    # so the stream must still carry it for the verifier to reject.
    stranger = stream(RUN % "TestStranger", PASS % "TestStranger")
    _, _, lines = select(helper, PATTERN, NAMES, stranger)
    expect("test outside the enrolled set", len(lines), 2)
    # An enrolled test the pattern does not select is dropped, with its events.
    _, _, lines = select(helper, "^(TestAlpha|TestGamma)$", NAMES,
                         stream(RUN % "TestBeta", PASS % "TestBeta", RUN % "TestAlpha"))
    expect("enrolled test the pattern skips", lines, [RUN % "TestAlpha"])
    # A subtest name outside the enrolled set is kept like any other stranger.
    _, _, lines = select(helper, "^(TestAlpha)$", NAMES, stream(RUN % "TestBeta/case two"))
    expect("unenrolled subtest", lines, [RUN % "TestBeta/case two"])
    # An enrolled subtest follows its parent: kept when the parent is selected,
    # dropped when it is not. Only the first slash splits parent from subtest.
    subs = ["TestAlpha", "TestAlpha/a/b", "TestBeta", "TestBeta/c"]
    _, _, lines = select(helper, "^(TestAlpha)$", subs,
                         stream(RUN % "TestAlpha/a/b", RUN % "TestBeta/c"))
    expect("enrolled subtests", lines, [RUN % "TestAlpha/a/b"])
    # A final line with no newline is dropped, as a bash read loop drops it.
    _, _, lines = select(helper, PATTERN, NAMES,
                         stream(RUN % "TestAlpha", PASS % "TestAlpha", last_newline=False))
    expect("unterminated last line", lines, [RUN % "TestAlpha"])
    # The call is rejected with status 9 when the pattern selects no enrolled
    # name, when it is not anchored, and when it is not a regular expression.
    status, err, _ = select(helper, "^(TestNope)$", NAMES, clean)
    expect("pattern selecting nothing", (status, "matched nothing" in err), (9, True))
    status, err, _ = select(helper, "(TestAlpha|TestZzz)", NAMES, clean)
    expect("unanchored pattern", (status, "not anchored" in err), (9, True))
    status, _, _ = select(helper, "^(TestAlpha", NAMES, clean)
    expect("invalid pattern", status, 9)
    return found


# MUTATIONS break one rule each; every one must make problems() report it.
MUTATIONS = {
    "keeps-skipped-tests": ("or test not in names):", "or True):"),
    "drops-unenrolled": ("or test not in names):", "):"),
    "subtest-follows-nothing": (
        'or (b"/" in test and pattern.search(test.split(b"/", 1)[0]))', ""),
    "subtest-uses-last-slash": ('test.split(b"/", 1)[0]', 'test.rsplit(b"/", 1)[0]'),
    "drops-lines-without-test": ("if (test is None or pattern.search(test)", "if (pattern.search(test or b'')"),
    "keeps-unterminated-line": ("    lines.pop()\n", ""),
    "decoy-unchecked": ("if pattern.search(DECOY.encode()):", "if False:"),
    "empty-selection-passes": ("if not any(pattern.search(name) for name in names):", "if False:"),
}


class FakeGoEventsTest(unittest.TestCase):
    """The selector keeps what a real go test -run would emit."""

    def test_clean_selector_passes_every_case(self) -> None:
        self.assertEqual(problems(HELPER), [])

    def test_each_seeded_mutation_is_caught(self) -> None:
        source = HELPER.read_text(encoding="utf-8")
        for name, (old, new) in MUTATIONS.items():
            with self.subTest(mutation=name):
                self.assertEqual(source.count(old), 1, f"anchor for {name} drifted")
                with tempfile.TemporaryDirectory() as temp:
                    copy = pathlib.Path(temp) / "mutant.py"
                    copy.write_text(source.replace(old, new), encoding="utf-8")
                    self.assertNotEqual(problems(copy), [], f"{name} survived")


if __name__ == "__main__":
    unittest.main()
