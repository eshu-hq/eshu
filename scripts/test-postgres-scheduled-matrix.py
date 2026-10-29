"""Matrix-shape tests for the sharded PostgreSQL scheduled live proofs."""

import importlib.util
import re
import sys
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parent.parent
LIBRARY = ROOT / "scripts/lib/postgres_scheduled_runner.py"
sys.path.insert(0, str(LIBRARY.parent))
SPEC = importlib.util.spec_from_file_location("postgres_scheduled_runner", LIBRARY)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class ScheduledMatrixTest(unittest.TestCase):
    """The sharded legs select disjoint groups and reunite in the aggregate."""

    def test_matrix_legs_own_disjoint_group_streams(self) -> None:
        workflow = (ROOT / MODULE.WORKFLOW).read_text()
        jobs = re.findall(
            r"(?m)^  ([a-z][a-z0-9-]*):\n", workflow.split("\njobs:\n", 1)[1]
        )
        self.assertEqual(jobs, [
            "scheduled-setup",
            "scheduled-freshness-scale",
            "scheduled-freshness-a",
            "scheduled-freshness-b",
            "scheduled-ack",
            "scheduled-bulk",
            "scheduled-storage",
            "scheduled-aggregate",
        ])
        for group in (
            "freshness_links_scale", "freshness_links_a", "freshness_links_b",
        ):
            self.assertIn(f"${{{{ runner.temp }}}}/{group}.jsonl", workflow)
        self.assertIn("merge-multiple: false", workflow)
        self.assertIn(
            "needs: [scheduled-setup, scheduled-freshness-scale, "
            "scheduled-freshness-a, scheduled-freshness-b, scheduled-ack, "
            "scheduled-bulk, scheduled-storage]",
            workflow,
        )
        inventory = MODULE.load_inventory()
        MODULE.verify_inventory(ROOT, inventory)

    def test_root_storage_build_runs_only_in_ack_and_storage_legs(self) -> None:
        """#9488: the root storage -race test build stays off the bulk leg.

        The first family to test ./internal/storage/postgres pays its -race
        test build, measured at 11 to 21 minutes. In the bulk leg that build
        put the job at its 40-minute cap, so every root-package family runs in
        the storage leg, and no third job may pay the build again.
        """
        workflow = (ROOT / MODULE.WORKFLOW).read_text()
        expanded = MODULE.expand_actions(workflow, ROOT)
        blocks = MODULE.job_blocks(expanded)
        MODULE.verify_root_storage_builds(blocks)
        self.assertEqual(
            MODULE.root_storage_groups(blocks["scheduled-storage"]),
            sorted(MODULE.STORAGE_GROUPS))
        self.assertEqual(MODULE.root_storage_groups(blocks["scheduled-bulk"]), [])
        seeded = dict(blocks)
        seeded["scheduled-bulk"] = blocks["scheduled-bulk"] + (
            "          go test -json -race -p=1 -count=1 -timeout=6m "
            "./internal/storage/postgres -run '^(TestSeeded)$' "
            "| tee \"${RUNNER_TEMP}/seeded.jsonl\"\n")
        with self.assertRaisesRegex(ValueError, "root storage -race build"):
            MODULE.verify_root_storage_builds(seeded)


if __name__ == "__main__":
    unittest.main()
