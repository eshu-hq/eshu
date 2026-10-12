#!/usr/bin/env python3
"""Exercise the live runner's real lifecycle with disposable command stubs."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class LifecycleTest(unittest.TestCase):
    """Failures cannot certify fresh backend coverage."""

    def run_fixture(self, down_fails: bool, test_fails: bool, cleanup_fails: bool = False, leftovers: str = "") -> tuple[subprocess.CompletedProcess[str], Path, tempfile.TemporaryDirectory[str]]:
        """Run production shell while retaining the fixture for assertions."""
        directory = tempfile.TemporaryDirectory()
        root = Path(directory.name)
        binaries = root / "bin"
        binaries.mkdir()
        commands = {
            "docker": """#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FIXTURE/docker.log"
if [[ "$*" == ps* && "$LEFTOVERS" == container ]]; then echo owned-container; fi
if [[ "$*" == "volume ls"* && "$LEFTOVERS" == volume ]]; then echo owned-volume; fi
for arg in "$@"; do
  if [[ "$arg" == down ]]; then
  echo down >>"$FIXTURE/down.log"
  if [[ "$DOWN_FAILS" == 1 ]]; then exit 1; fi
  if [[ "$CLEANUP_FAILS" == 1 && $(wc -l <"$FIXTURE/down.log") -ge 2 ]]; then exit 1; fi
fi
done
exit 0
""",
            "curl": "#!/usr/bin/env bash\nexit 0\n",
            "go": """#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FIXTURE/go.log"
if [[ "$TEST_FAILS" == 1 && $(wc -l <"$FIXTURE/go.log") -eq 1 ]]; then exit 1; fi
exit 0
""",
        }
        for name, script in commands.items():
            target = binaries / name
            target.write_text(script)
            target.chmod(0o755)
        environment = dict(os.environ, PATH=f"{binaries}:{os.environ['PATH']}", FIXTURE=str(root),
                           DOWN_FAILS=str(int(down_fails)), TEST_FAILS=str(int(test_fails)),
                           CLEANUP_FAILS=str(int(cleanup_fails)), LEFTOVERS=leftovers,
                           ESHU_SKIP_LIVE_GATE_LOCK="1", ESHU_CI_IMAGES="0")
        # The CI compose wrapper refuses to run in CI without CI image mode; this
        # fixture runs the runner with the mode off and stub docker, as a laptop does.
        environment.pop("GITHUB_ACTIONS", None)
        result = subprocess.run(["bash", str(ROOT / "scripts/run-live-backend-tests.sh"),
                                 "--shard", "1/4", "--results", str(root / "result.json")],
                                env=environment, capture_output=True, text=True, check=False)
        return result, root, directory

    def test_failed_down_never_starts_tests(self) -> None:
        result, root, directory = self.run_fixture(True, False)
        with directory:
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("fresh", result.stderr)
            self.assertFalse((root / "go.log").exists())

    def test_leftover_resources_never_start_tests(self) -> None:
        """A successful compose command alone cannot prove an empty stack."""
        for leftover in ("container", "volume"):
            with self.subTest(leftover=leftover):
                result, root, directory = self.run_fixture(False, False, leftovers=leftover)
                with directory:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertFalse((root / "go.log").exists())
                    self.assertFalse(json.loads((root / "result.json").read_text())["ok"])

    def test_failed_cleanup_cannot_report_success(self) -> None:
        """A tested target is incomplete until its owned resources are gone."""
        result, root, directory = self.run_fixture(False, False, cleanup_fails=True)
        with directory:
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(len((root / "go.log").read_text().splitlines()), 1)
            self.assertFalse(json.loads((root / "result.json").read_text())["ok"])

    def test_failed_test_attempts_rest_and_reports_failure(self) -> None:
        selected = subprocess.check_output(
            ["bash", str(ROOT / "scripts/run-live-backend-tests.sh"), "--shard", "1/4"],
            env=dict(os.environ, ESHU_LIVE_RUNNER_SELFTEST="plan"), text=True).splitlines()
        result, root, directory = self.run_fixture(False, True)
        with directory:
            self.assertNotEqual(result.returncode, 0)
            report = json.loads((root / "result.json").read_text())
            self.assertFalse(report["ok"])
            self.assertEqual(len(report["runs"]), len(selected))
            self.assertEqual(sum(row["status"] != 0 for row in report["runs"]), 1)
            self.assertEqual(len((root / "go.log").read_text().splitlines()), len(selected))
            self.assertTrue(all("-count=1" in line for line in (root / "go.log").read_text().splitlines()))


if __name__ == "__main__":
    unittest.main()
