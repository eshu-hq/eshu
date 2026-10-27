#!/usr/bin/env python3
"""Pin the reviewed docs-only CI carve-out across enrollment guards."""

import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))

from replay_tier_enrollment import verify_routes  # noqa: E402
from replay_tier_fixtures import copy_route_sources  # noqa: E402
from replay_tier_routes import (  # noqa: E402
    _reject_shadowed_carveout,
    workflow_negations,
    workflow_paths,
)


LIB = Path(__file__).parent / "lib" / "nornic_conformance_events.py"
SPEC = importlib.util.spec_from_file_location("nornic_conformance_events", LIB)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)

ROOT = Path(__file__).resolve().parent.parent


class LiveBackendDocsCarveoutTest(unittest.TestCase):
    """Pin the reviewed markdown narrowing in the live-backend fragment."""

    def test_negation_narrows_scheduling_without_touching_enrollment(self) -> None:
        fragment = (ROOT / "specs/ci-pr-workflows.d/live-backend-tests.yml").read_text()
        positives = workflow_paths(fragment, "live-backend-tests.yml")
        self.assertIn("specs/live-tests.d/**", positives)
        self.assertIn("go/**", positives)
        self.assertNotIn("**/*.md", positives)
        self.assertEqual(
            workflow_negations(fragment, "live-backend-tests.yml"), {"**/*.md"})

    def test_unreviewed_negation_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary)
            copy_route_sources(fixture)
            path = fixture / "specs/ci-pr-workflows.d/live-backend-tests.yml"
            mutated = path.read_text().replace(
                "  - '!**/*.md'\n", "  - '!**/*.yaml'\n", 1)
            self.assertNotEqual(mutated, path.read_text())
            path.write_text(mutated)
            with self.assertRaisesRegex(ValueError, "carve-out"):
                verify_routes(fixture)

    def test_md_replay_source_trips_the_carveout_pin(self) -> None:
        with self.assertRaisesRegex(ValueError, "shadows a replay source"):
            _reject_shadowed_carveout({"**/*.md"}, {"docs/proof.md"})
        _reject_shadowed_carveout({"**/*.md"}, {"scripts/verify-replay-tier.sh"})

    def test_replay_tier_fragment_still_rejects_negations(self) -> None:
        fragment = (ROOT / "specs/ci-pr-workflows.d/verify-replay-tier.yml").read_text()
        mutated = fragment.replace("  jobs:\n", "  - '!**/*.md'\n  jobs:\n", 1)
        self.assertNotEqual(mutated, fragment)
        with self.assertRaisesRegex(ValueError, "ambiguous path sequence"):
            workflow_paths(mutated, "verify-replay-tier.yml")


class NornicConformanceDocsCarveoutTest(unittest.TestCase):
    """Pin the reviewed markdown narrowing in the conformance routing."""

    def test_docs_carveout_narrows_scheduling_without_touching_enrollment(self) -> None:
        """The reviewed markdown narrowing must not select or deselect conformance routes."""
        root = Path(__file__).resolve().parent.parent
        e2e = (root / MODULE.WORKFLOW).read_text()
        live = (root / ".github/workflows/live-backend-tests.yml").read_text()
        registry = (root / "specs/ci-gates.d/gates-09.yaml").read_text()
        e2e_fragment = (root / "specs/ci-pr-workflows.d/e2e-tests.yml").read_text()
        live_fragment = (root / "specs/ci-pr-workflows.d/live-backend-tests.yml").read_text()
        MODULE.verify_routing_text(e2e, live, registry, e2e_fragment, live_fragment)
        self.assertIn("**/*.md", MODULE._fragment_narrowings(e2e_fragment))
        self.assertIn("**/*.md", MODULE._fragment_narrowings(live_fragment))
        for label, fragment in (("e2e", e2e_fragment), ("live", live_fragment)):
            with self.subTest(label=label):
                mutated = fragment.replace(
                    "  - '!**/*.md'\n", "  - '!**/*.yaml'\n", 1)
                self.assertNotEqual(mutated, fragment)
                with self.assertRaisesRegex(ValueError, "carve-out"):
                    MODULE.verify_routing_text(
                        e2e, live, registry,
                        mutated if label == "e2e" else e2e_fragment,
                        mutated if label == "live" else live_fragment)


if __name__ == "__main__":
    unittest.main()
