#!/usr/bin/env python3
"""Bind the two PostgreSQL B-7 identity proofs to sources and Go JSON events."""

import argparse
import json
from pathlib import Path
import re
import sys


# This closed mapping is reviewed with the ledger. It is intentionally not a
# configurable list: adding a proof also requires a real B-7 helper invocation.
PROOFS = {
    "go/internal/reducer/containerimage/"
    "container_image_identity_support_writer_demotion_postgres_live_test.go": (
        "github.com/eshu-hq/eshu/go/internal/reducer/containerimage",
        "TestContainerImageIdentitySupportWriterRetiresPromotedDecisionOnDemotionLive",
        "run_container_image_identity_demotion_proof",
    ),
    "go/internal/storage/postgres/"
    "container_image_identity_cloud_reopen_ordering_postgres_live_test.go": (
        "github.com/eshu-hq/eshu/go/internal/storage/postgres",
        "TestContainerImageIdentityCloudReferenceReopenOrderingPostgresLive",
        "run_container_image_identity_cloud_reopen_ordering_proof",
    ),
}
HELPER = Path("scripts/lib/golden-corpus-container-image-demotion.sh")
DRIVER = Path("scripts/verify-golden-corpus-gate.sh")

INPUTS = (
    "scripts/lib/golden_corpus_identity.py",
    "scripts/test-golden-corpus-identity.py",
    "scripts/lib/verify-live-tests-ledger.py",
    "scripts/lib/live_tests_registry.py",
    "scripts/verify-live-tests-ledger.sh",
    "specs/live-tests.v1.yaml",
    "specs/live-tests.d/**",
)


def verify_routes(root: Path) -> None:
    """Require the existing PR, main push and merge-group B-7 execution routes.

    Args:
        root: Checkout containing the workflow and gate registry fragments.

    Raises:
        ValueError: An input is unselected or an owning job/step can be skipped.
        OSError: A required workflow or registry fragment cannot be read.
    """
    workflow = (root / ".github/workflows/golden-corpus-gate.yml").read_text()
    activation = ("  workflow_call:\n    inputs:\n"
                  "      plan_jobs:\n        required: true\n        type: string\n"
                  "      plan_gates:\n        required: true\n        type: string\n"
                  "      plan_mode:\n        required: true\n        type: string\n"
                  "  merge_group:\n    types: [checks_requested]\n"
                  "  push:\n    branches: [ main ]\n")
    uncommented = "\n".join(line for line in workflow.splitlines()
                              if not line.lstrip().startswith("#")) + "\n"
    if "on:\n" + activation not in uncommented or "  pull_request:" in uncommented:
        raise ValueError("B-7 identity workflow activation changed")
    fragment = (root / "specs/ci-pr-workflows.d/golden-corpus-gate.yml").read_text()
    active = [line for line in fragment.splitlines()
              if line.strip() and not line.lstrip().startswith("#")]
    prefix = ["schema_version: 1", "workflows:",
              "- file: golden-corpus-gate.yml", "  name: Golden Corpus Gate",
              "  synchronize: true", "  branches:", "  - main", "  paths:"]
    if active[:len(prefix)] != prefix or active.count("  paths:") != 1:
        raise ValueError("B-7 identity fragment activation changed")
    end = active.index("  jobs:") if "  jobs:" in active else -1
    if end < len(prefix) or active[end + 1:] != [
            "  - corpus-gate", "  - differential", "  - differential-capture",
            "  - queue-selection", "  - static-mirror", "  job_names:",
            "    corpus-gate: corpus-gate (${{ matrix.graph_backend }})",
            "    differential: differential nornicdb vs neo4j",
            "    differential-capture: differential capture (pair ${{ matrix.pair }}, ${{ matrix.graph_backend }})",
            "    static-mirror: static mirror"]:
        raise ValueError("B-7 identity fragment jobs changed")
    pr_paths = active[len(prefix):end]
    if any(not line.startswith("  - ") for line in pr_paths):
        raise ValueError("B-7 identity fragment paths changed")
    for path in INPUTS:
        if pr_paths.count(f"  - {path}") != 1:
            raise ValueError(f"B-7 identity workflow input unselected: {path}")
    for job, step, command, extra in (
        ("static-mirror", "Validate orchestrator contract",
         "bash scripts/test-verify-golden-corpus-gate.sh", ""),
        ("corpus-gate", "Run B-7 golden corpus gate",
         "bash scripts/verify-golden-corpus-gate.sh",
         "        env:\n          ESHU_GRAPH_BACKEND: ${{ matrix.graph_backend }}\n"
         "          ESHU_NEO4J_PASSWORD: change-me\n          ESHU_POSTGRES_PASSWORD: change-me\n"),
    ):
        body = re.search(rf"(?ms)^  {job}:\n(.*?)(?=^  [a-z][\w-]*:|\Z)", workflow)
        if body is None:
            raise ValueError(f"B-7 identity owner job missing: {job}")
        header = body.group(1).split("    steps:\n", 1)[0]
        condition = ("    if: ${{ github.event_name != 'pull_request' || "
                     f"contains(fromJSON(inputs.plan_jobs || '[]'), '{job}') }}}}")
        if (header.splitlines().count("    needs: queue-selection") != 1
                or re.findall(r"(?m)^    if:.*$", header) != [condition]
                or re.search(r"(?m)^    continue-on-error:", header)):
            raise ValueError(f"B-7 identity owner job inactive: {job}")
        selection = "static mirror" if job == "static-mirror" else "corpus-gate"
        expected = (f"      - name: {step}\n"
                    "        if: ${{ github.event_name != 'merge_group' || "
                    "contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), '"
                    + selection + "') }}\n" + extra + f"        run: {command}\n")
        actual = re.findall(rf"(?ms)^      - name: {re.escape(step)}\n.*?(?=^      -|\Z)",
                            body.group(1))
        if len(actual) != 1 or actual[0].rstrip() != expected.rstrip():
            raise ValueError(f"B-7 identity owner step is not mandatory: {step}")
    for fragment, gate in (("07", "golden-corpus-mirror"),
                           ("08", "golden-corpus-gate"),
                           ("08", "golden-corpus-gate-neo4j")):
        text = (root / f"specs/ci-gates.d/gates-{fragment}.yaml").read_text()
        blocks = re.findall(rf"(?ms)^  - id: {gate}\n(.*?)(?=^  - id:|\Z)", text)
        if len(blocks) != 1:
            raise ValueError(f"B-7 identity registry owner missing: {gate}")
        triggers = blocks[0].split("    triggers:\n", 1)[1].split("    local:\n", 1)[0]
        for path in INPUTS:
            if triggers.splitlines().count(f'      - "{path}"') != 1:
                raise ValueError(f"B-7 identity registry input unselected: {gate}: {path}")
        if "      workflow: golden-corpus-gate.yml" not in blocks[0] and (
                '      workflow: "golden-corpus-gate.yml"' not in blocks[0]):
            raise ValueError(f"B-7 identity registry workflow changed: {gate}")


def verify_inventory(root: Path) -> None:
    """Reject changed source test sets and inactive B-7 helper invocations.

    Args:
        root: Checkout containing the canonical driver and unchanged Go tests.

    Raises:
        ValueError: A mapped source or its mandatory invocation has changed.
        OSError: A required source cannot be read.
    """
    helper = (root / HELPER).read_text(encoding="utf-8")
    driver = (root / DRIVER).read_text(encoding="utf-8")
    for relative, (package, name, function) in PROOFS.items():
        source = (root / relative).read_text(encoding="utf-8")
        actual = re.findall(r"^func (Test\w+)\(", source, re.M)
        if actual != [name] or re.search(r"^//go:build ", source, re.M):
            raise ValueError(f"B-7 identity source inventory changed: {relative}")
        expected_package = "github.com/eshu-hq/eshu/" + relative.rsplit("/", 1)[0]
        if package != expected_package:
            raise ValueError(f"B-7 identity package mismatch: {relative}")
        if re.findall(rf"(?m)^.*\b{function}\b.*$", driver) != [function]:
            raise ValueError(f"B-7 identity invocation must run unconditionally: {function}")
        block = re.search(rf"(?ms)^{function}\(\) \{{\n(.*?)^\}}$", helper)
        if block is None or block.group(1).count(f'\ttest_name="{name}"\n') != 1:
            raise ValueError(f"B-7 identity helper selector changed: {function}")
        guard = '\tgolden_corpus_exact_test_passed "${proof_json}" "${test_name}" ||\n\t\tdie '
        if block.group(1).count(guard) != 1:
            raise ValueError(f"B-7 identity event guard missing: {function}")


def verify_events(path: Path, name: str) -> None:
    """Require ordered selected run/pass and successful package completion.

    Args:
        path: Raw, stdout-only Go JSON stream; unrelated reducer packages may occur.
        name: One exact test from the closed B-7 identity mapping.

    Raises:
        ValueError: Invalid JSON, wrong identity, skipped/failed or duplicate proof.
        OSError: The captured stream cannot be read.
    """
    packages = [package for package, test, _ in PROOFS.values() if test == name]
    if len(packages) != 1:
        raise ValueError(f"unknown B-7 identity proof: {name}")
    package = packages[0]
    selected: list[str] = []
    with path.open(encoding="utf-8") as stream:
        for number, line in enumerate(stream, 1):
            event = json.loads(line)
            if not isinstance(event, dict) or not isinstance(event.get("Package"), str):
                raise ValueError(f"invalid Go event at line {number}")
            action, test = event.get("Action"), event.get("Test")
            if action == "fail":
                raise ValueError(f"failed Go event at line {number}")
            if test is not None and not isinstance(test, str):
                raise ValueError(f"invalid Go test at line {number}")
            if test == name or (test and test.startswith(name + "/")):
                if event["Package"] != package or action == "skip":
                    raise ValueError(f"wrong or skipped selected proof at line {number}")
            if event["Package"] != package:
                continue
            if test == name and action in {"run", "pass", "skip"}:
                selected.append(action)
            elif test is None and action in {"pass", "skip"}:
                selected.append("package-" + action)
    if selected != ["run", "pass", "package-pass"]:
        raise ValueError(f"B-7 identity proof {name}: invalid terminals {selected}")


def main() -> int:
    """Validate source enrollment or the stdout captured by a B-7 proof helper."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--events", type=Path)
    parser.add_argument("--test")
    args = parser.parse_args()
    try:
        if args.events is None:
            verify_inventory(args.repo_root)
            verify_routes(args.repo_root)
        elif args.test:
            verify_events(args.events, args.test)
        else:
            parser.error("--test is required with --events")
    except (OSError, ValueError) as error:
        print(error, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
