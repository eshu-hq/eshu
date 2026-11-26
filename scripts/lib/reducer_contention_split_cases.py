"""Cases for the two-owner-job split of the reducer contention gate (#11252)."""

from pathlib import Path
import shutil
import tempfile
import unittest


def build(module):
    """Return the TestCase bound to the loaded reducer_contention_events module."""

    class TwoJobSplitTest(unittest.TestCase):
        """The storage/postgres suite runs as two owner jobs that must stay disjoint."""

        repo = Path(__file__).resolve().parents[2]
        job_b = "  contention-gate-storage-b:\n"

        def check(self, mutate, expected: str) -> None:
            """Apply mutate to the real workflow and require verify_inventory to refuse."""
            inventory = module.load_inventory()
            original = (self.repo / module.WORKFLOW).read_text()
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                workflow = root / module.WORKFLOW
                workflow.parent.mkdir(parents=True)
                for relative in inventory:
                    target = root / relative
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(self.repo / relative, target)
                workflow.write_text(original)
                module.verify_inventory(root, inventory)  # GREEN: the real workflow
                workflow.write_text(mutate(original))
                with self.assertRaisesRegex(ValueError, expected):
                    module.verify_inventory(root, inventory)

        def test_halves_must_not_overlap(self) -> None:
            """RED: a proof selected by both halves would run twice on two runners."""

            def overlap(text: str) -> str:
                marker = "      - name: Run supply-chain and holder fan-out proofs\n"
                start = text.index(marker)
                return text[:start] + text[start:].replace(
                    "-run '^(", "-run '^(TestReducerContentionGate|", 1
                ).replace("--select '^(", "--select '^(TestReducerContentionGate|", 1)

            self.check(overlap, "both halves")

        def test_a_proof_dropped_from_both_halves_is_refused(self) -> None:
            """RED: removing a proof's alternative from the half that selects it loses it."""

            def drop(step: str, alternative: str):
                def mutate(text: str) -> str:
                    marker = f"      - name: {step}\n"
                    start = text.index(marker)
                    end = text.index("\n      - name:", start + len(marker))
                    block = text[start:end]
                    self.assertEqual(block.count(alternative), 2)  # -run and --select
                    return text[:start] + block.replace(alternative, "") + text[end:]

                return mutate

            self.check(
                drop("Run reducer contention gate", "TestReducerContentionGate|"),
                "unselected tests",
            )
            self.check(
                drop(
                    "Run supply-chain and holder fan-out proofs",
                    "TestSuppression|",
                ),
                "unselected tests",
            )

        def test_a_step_timeout_above_the_bound_is_refused(self) -> None:
            """RED: a cap of 85000 s defeats the job-cap arithmetic; GREEN is the real 1200 s."""
            self.check(
                lambda text: text.replace("-timeout 1200s", "-timeout 85000s", 1),
                "exceeds the 1200s bound",
            )

        def test_second_job_cannot_be_dropped_or_suppressed(self) -> None:
            """The second job is held to the same fail-closed grammar as the first."""
            self.check(lambda text: text[: text.index(self.job_b)], "noncanonical jobs")
            for label, line in (
                ("disabled", "    if: ${{ false }}\n"),
                ("continued", "    continue-on-error: true\n"),
                ("needs smuggled in", "    needs: [other]\n"),
                ("shell suppressed", "    defaults:\n      run:\n        shell: bash {0} || true\n"),
            ):
                with self.subTest(label=label):
                    self.check(
                        lambda text, line=line: text.replace(self.job_b, self.job_b + line, 1),
                        "inactive|fail-closed",
                    )

    return TwoJobSplitTest
