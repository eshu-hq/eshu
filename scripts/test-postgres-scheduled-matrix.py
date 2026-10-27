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
            "scheduled-bulk]",
            workflow,
        )
        inventory = MODULE.load_inventory()
        MODULE.verify_inventory(ROOT, inventory)


if __name__ == "__main__":
    unittest.main()
