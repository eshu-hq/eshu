"""Per-change drift gate cases for the readiness weights (#10171).

The unweighted-test count used to be one pool shared by every PR and enforced
on each PR's merge result. Feature PRs may not edit the weights, so each one
only drew the pool down, and once main crossed the limit every later PR failed
whatever it added. The gate now counts what the change itself leaves without a
weight, measured against its own base commit, and reports the pool as a
non-blocking signal. These cases pin that contract on synthetic drift and on
throwaway git repositories, so they never depend on the live plan's size.
"""

from __future__ import annotations

import io
import json
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

import live_postgres_readiness_weights as wt
import live_postgres_readiness_weights_delta as delta

LIMIT = wt.MAX_DEFAULTED_TESTS
RESULTS_SCRIPT = "scripts/lib/live_postgres_readiness_results.py"
WEIGHTS_FILE = "specs/live-postgres-readiness-weights.v1.json"


def names(count: int, prefix: str = "TestPool",
          package: str = "./internal/pool") -> tuple[str, ...]:
    """Return ``count`` distinct flat plan lines in one package."""
    return tuple(f"{package}|{prefix}{index}Live" for index in range(count))


def drift(tests: tuple[str, ...] = (), packages: tuple[str, ...] = ()) -> wt.Drift:
    """Build a drift report with the given unweighted tests and packages."""
    return wt.Drift(defaulted_tests=tests, defaulted_packages=packages, stale_tests=())


class DeltaGateTest(unittest.TestCase):
    """The blocking check counts only what the change adds to the pool."""

    def test_change_adding_three_tests_passes_when_main_is_over_the_cap(self) -> None:
        # #10186: main already had 44 unweighted tests and the PR added 3.
        pool = names(44)
        head = drift(pool + names(3, "TestMine", "./internal/mine"))
        self.assertTrue(wt.drift_errors(head), "the retired global gate fails this")
        self.assertEqual(wt.delta_errors(head, drift(pool)), [])

    def test_change_adding_one_test_passes_when_main_sits_at_the_cap(self) -> None:
        # #10119: main was at exactly 40, the PR added 1.
        pool = names(LIMIT)
        head = drift(pool + names(1, "TestMine", "./internal/mine"))
        self.assertTrue(wt.drift_errors(head))
        self.assertEqual(wt.delta_errors(head, drift(pool)), [])

    def test_change_adding_none_passes_when_the_pool_is_far_over_the_cap(self) -> None:
        pool = names(52)
        self.assertTrue(wt.drift_errors(drift(pool)))
        self.assertEqual(wt.delta_errors(drift(pool), drift(pool)), [])

    def test_change_adding_more_than_the_limit_fails_and_names_them(self) -> None:
        pool = names(3)
        mine = names(LIMIT + 1, "TestMine", "./internal/mine")
        errors = wt.delta_errors(drift(pool + mine), drift(pool))
        self.assertEqual(len(errors), 1, errors)
        self.assertRegex(errors[0], rf"{LIMIT + 1} tests this change adds have no weight "
                                    rf"\(limit {LIMIT}\)")
        self.assertIn("TestMine0Live", errors[0])
        self.assertIn("must not edit", errors[0])

    def test_change_adding_exactly_the_limit_passes(self) -> None:
        mine = names(LIMIT, "TestMine", "./internal/mine")
        self.assertEqual(wt.delta_errors(drift(mine), drift()), [])

    def test_a_weight_removed_by_the_change_counts_as_added(self) -> None:
        # Present at base with a weight, unweighted at head: the change did it.
        lost = names(LIMIT + 1, "TestWeighted", "./internal/was")
        errors = wt.delta_errors(drift(lost), drift())
        self.assertEqual(len(errors), 1, errors)

    def test_a_refit_that_prices_the_pool_passes(self) -> None:
        pool = names(52)
        self.assertEqual(wt.delta_errors(drift(), drift(pool)), [])

    def test_new_unweighted_packages_are_counted_against_their_own_limit(self) -> None:
        pool = tuple(f"./internal/old{index}" for index in range(5))
        added = tuple(f"./internal/new{index}" for index in range(wt.MAX_DEFAULTED_PACKAGES + 1))
        self.assertEqual(wt.delta_errors(drift(packages=pool), drift(packages=pool)), [])
        errors = wt.delta_errors(drift(packages=pool + added), drift(packages=pool))
        self.assertEqual(len(errors), 1, errors)
        self.assertRegex(errors[0], rf"{len(added)} packages this change adds have no cost "
                                    rf"\(limit {wt.MAX_DEFAULTED_PACKAGES}\)")

    def test_pool_signal_reports_staleness_without_gating(self) -> None:
        self.assertIsNone(wt.pool_signal(drift(names(LIMIT))))
        signal = wt.pool_signal(drift(names(52)))
        assert signal is not None
        self.assertIn("52 planned tests have no weight", signal)
        self.assertIn("not a merge gate", signal)


class PoolSignalAnnotationTest(unittest.TestCase):
    """The stale-pool warning must reach the Actions runner as a command."""

    def emitted(self, environ: dict[str, str]) -> str:
        """Return what a unittest run writes: progress dots, then the signal."""
        stream = io.StringIO()
        stream.write("..........")  # unittest prints dots with no newline
        wt.emit_pool_signal("52 planned tests have no weight", environ, stream)
        return stream.getvalue()

    def test_the_annotation_starts_its_own_line_after_the_progress_dots(self) -> None:
        # The runner reads a workflow command only at the start of a line, so a
        # warning glued to the dots stays plain text in a green step.
        lines = self.emitted({"GITHUB_ACTIONS": "true"}).splitlines()
        commands = [line for line in lines if line.startswith("::warning title=")]
        self.assertEqual(len(commands), 1, lines)
        self.assertIn("readiness weights are stale", commands[0])

    def test_outside_actions_the_signal_is_a_plain_note_on_its_own_line(self) -> None:
        lines = self.emitted({}).splitlines()
        self.assertEqual([line for line in lines if line.startswith("note: ")],
                         ["note: 52 planned tests have no weight"], lines)
        self.assertFalse(any("::warning" in line for line in lines), lines)


def git(repo: Path, *args: str, env: dict[str, str] | None = None) -> str:
    """Run git in ``repo`` and return stdout, failing the test on error."""
    proc = subprocess.run(["git", "-C", str(repo), "-c", "user.name=t", "-c", "user.email=t@t",
                           "-c", "commit.gpgsign=false", *args],
                          text=True, capture_output=True, check=False, env=env)
    if proc.returncode != 0:
        raise AssertionError(f"git {args}: {proc.stderr}")
    return proc.stdout.strip()


def commit(repo: Path, name: str, body: str = "x") -> str:
    """Write one file, commit it, and return the commit id."""
    path = repo / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body, encoding="utf-8")
    git(repo, "add", "-A")
    git(repo, "commit", "-q", "-m", name)
    return git(repo, "rev-parse", "HEAD")


class BaseResolutionTest(unittest.TestCase):
    """The base is a fixed commit, never a moving remote tip."""

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = Path(self.tmp.name)
        git(self.repo, "init", "-q", "-b", "main")

    def test_merge_ref_resolves_to_its_first_parent_not_the_moved_main_tip(self) -> None:
        # GitHub builds refs/pull/N/merge from main's tip at that moment. If
        # main moves before the job runs, the merge ref's parent 1 is still
        # the state the PR was combined with (#10131 shape).
        first = commit(self.repo, "a.txt")
        git(self.repo, "checkout", "-q", "-b", "pr")
        commit(self.repo, "pr.txt")
        git(self.repo, "checkout", "-q", "main")
        git(self.repo, "merge", "-q", "--no-ff", "-m", "merge ref", "pr")
        merged = git(self.repo, "rev-parse", "HEAD")
        git(self.repo, "checkout", "-q", "--detach", merged)
        git(self.repo, "update-ref", "refs/remotes/origin/main", merged)
        moved = git(self.repo, "commit-tree", first + "^{tree}", "-p", first, "-m", "later")
        git(self.repo, "update-ref", "refs/remotes/origin/main", moved)
        base = delta.resolve_base(self.repo, {"GITHUB_EVENT_NAME": "pull_request"})
        self.assertEqual(base.commit, first)
        self.assertNotEqual(base.commit, moved)

    def test_a_local_merge_commit_is_not_mistaken_for_a_merge_ref(self) -> None:
        # Without a pull_request event, a merge the developer made locally has
        # its own branch as parent 1; only the merge base with origin/main is
        # the change's start.
        first = commit(self.repo, "a.txt")
        git(self.repo, "update-ref", "refs/remotes/origin/main", first)
        git(self.repo, "checkout", "-q", "-b", "topic")
        commit(self.repo, "t.txt")
        git(self.repo, "checkout", "-q", "main")
        commit(self.repo, "m.txt")
        git(self.repo, "checkout", "-q", "topic")
        git(self.repo, "merge", "-q", "--no-ff", "-m", "local merge", "main")
        self.assertEqual(delta.resolve_base(self.repo, {}).commit, first)

    def test_push_event_resolves_to_the_previous_commit(self) -> None:
        first = commit(self.repo, "a.txt")
        commit(self.repo, "b.txt")
        base = delta.resolve_base(self.repo, {"GITHUB_EVENT_NAME": "push"})
        self.assertEqual(base.commit, first)

    def test_local_branch_resolves_to_the_merge_base_with_origin_main(self) -> None:
        first = commit(self.repo, "a.txt")
        git(self.repo, "update-ref", "refs/remotes/origin/main", first)
        commit(self.repo, "b.txt")
        commit(self.repo, "c.txt")
        self.assertEqual(delta.resolve_base(self.repo, {}).commit, first)

    def test_override_wins(self) -> None:
        first = commit(self.repo, "a.txt")
        commit(self.repo, "b.txt")
        commit(self.repo, "c.txt")
        base = delta.resolve_base(self.repo, {"ESHU_READINESS_DRIFT_BASE": first})
        self.assertEqual(base.commit, first)

    def test_unresolvable_override_fails_closed(self) -> None:
        commit(self.repo, "a.txt")
        with self.assertRaisesRegex(delta.BaseUnavailable, "ESHU_READINESS_DRIFT_BASE"):
            delta.resolve_base(self.repo, {"ESHU_READINESS_DRIFT_BASE": "deadbeef"})

    def test_depth_one_checkout_never_reads_a_ref_equal_to_head_as_an_empty_change(self) -> None:
        head = commit(self.repo, "a.txt")
        git(self.repo, "update-ref", "refs/remotes/origin/main", head)
        for event in ("pull_request", "push"):
            with self.assertRaisesRegex(delta.BaseUnavailable, "fetch-depth: 2"):
                delta.resolve_base(self.repo, {"GITHUB_EVENT_NAME": event})
        # A local run has no event and keeps the merge-base answer.
        self.assertEqual(delta.resolve_base(self.repo, {}).commit, head)

    def test_no_history_fails_closed_instead_of_passing_silently(self) -> None:
        commit(self.repo, "a.txt")
        with self.assertRaisesRegex(delta.BaseUnavailable, "fetch-depth"):
            delta.resolve_base(self.repo, {"GITHUB_EVENT_NAME": "pull_request"})


def plant_inventory(repo: Path, tests: dict[str, list[str]], weighted: list[str],
                    exit_code: int = 0) -> None:
    """Write a stub ``list-packages`` CLI and a v1 weights file into ``repo``."""
    rows = [f"{package}\t^({'|'.join(found)})$\t{' '.join(found)}"
            for package, found in tests.items()]
    script = repo / RESULTS_SCRIPT
    script.parent.mkdir(parents=True, exist_ok=True)
    script.write_text(f"import sys\nfor row in {json.dumps(rows)}:\n    print(row)\n"
                      f"sys.exit({exit_code})\n", encoding="utf-8")
    weights = repo / WEIGHTS_FILE
    weights.parent.mkdir(parents=True, exist_ok=True)
    weights.write_text(json.dumps({
        "schema_version": 1, "source_runs": ["1"], "fixed_job_seconds": 60,
        "cold_cache_seconds": 30, "default_test_seconds": 5, "default_package_seconds": 20,
        "package_seconds": {package: 10 for package in tests},
        "test_seconds": {line: 1 for line in weighted}}), encoding="utf-8")


class LoadBaseDriftTest(unittest.TestCase):
    """The base drift comes from the base commit's own plan and weights."""

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = Path(self.tmp.name)
        git(self.repo, "init", "-q", "-b", "main")

    def test_base_unweighted_tests_come_from_the_base_commit(self) -> None:
        plant_inventory(self.repo, {"./internal/a": ["TestOneLive", "TestTwoLive"]},
                        weighted=["./internal/a|TestOneLive"])
        base = commit(self.repo, "marker")
        plant_inventory(self.repo, {"./internal/a": ["TestOneLive", "TestTwoLive", "TestNewLive"]},
                        weighted=["./internal/a|TestOneLive", "./internal/a|TestTwoLive"])
        commit(self.repo, "head")
        loaded = delta.load_base_drift(self.repo, base)
        self.assertEqual(loaded.defaulted_tests, ("./internal/a|TestTwoLive",))
        self.assertEqual(loaded.defaulted_packages, ())

    def test_a_base_whose_plan_cannot_be_listed_fails_closed(self) -> None:
        plant_inventory(self.repo, {"./internal/a": ["TestOneLive"]}, weighted=[], exit_code=1)
        base = commit(self.repo, "marker")
        with self.assertRaisesRegex(delta.BaseUnavailable, "list-packages"):
            delta.load_base_drift(self.repo, base)

    def test_a_base_without_weights_fails_closed(self) -> None:
        plant_inventory(self.repo, {"./internal/a": ["TestOneLive"]}, weighted=[])
        (self.repo / WEIGHTS_FILE).unlink()
        base = commit(self.repo, "marker")
        with self.assertRaisesRegex(delta.BaseUnavailable, "weights"):
            delta.load_base_drift(self.repo, base)


WORKFLOW = Path(__file__).resolve().parents[2] / ".github/workflows/live-postgres-readiness.yml"


def hermetic_checkout_depth(workflow: str) -> str | None:
    """Return the ``fetch-depth`` of the hermetic job's checkout step, if set."""
    job = workflow.split("\n  hermetic:\n", 1)[1].split("\n  readiness-plan:\n", 1)[0]
    step = job.split("\n      - name: Check out code\n", 1)[1].split("\n      - name:", 1)[0]
    found = re.search(r"(?m)^          fetch-depth: (\S+)$", step)
    return found.group(1) if found else None


class WorkflowWiringTest(unittest.TestCase):
    """The hermetic job keeps the history the delta gate needs."""

    def test_hermetic_checkout_keeps_the_merge_refs_parents(self) -> None:
        workflow = WORKFLOW.read_text(encoding="utf-8")
        self.assertEqual(hermetic_checkout_depth(workflow), "2")

    def test_a_depth_one_checkout_is_detected(self) -> None:
        workflow = WORKFLOW.read_text(encoding="utf-8")
        shallow = workflow.replace("          fetch-depth: 2\n", "", 1)
        self.assertNotEqual(shallow, workflow)
        self.assertIsNone(hermetic_checkout_depth(shallow))


if __name__ == "__main__":
    unittest.main()
