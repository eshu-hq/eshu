#!/usr/bin/env python3
"""Verify exact Ifá scheduled source enrollment and captured Go JSON events."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import sys

from reducer_contention_events import verify_events
from ifa_scheduled_manifest import NORNIC_DIGEST, POSTGRES_DIGEST


INVENTORY = Path(__file__).with_name("ifa_scheduled_inventory.json")
RUNNER = Path("scripts/run-ifa-scheduled-proofs.sh")
WORKFLOW = Path(".github/workflows/ifa-scheduled-proofs.yml")
FRAGMENT = Path("specs/ci-pr-workflows.d/ifa-scheduled-proofs.yml")
WORKFLOW_SHA256 = "a3213999c2e28a1231fd3472042cc9ff958cc4144f68d7278e93f536b8cc86aa"
RUNNER_SHA256 = "63e6573b21b77cdab837b15080672368f53d97d47d6cb903c6037f663576a03e"
TEST_PATTERN = re.compile(r"^func (Test\w+)\(", re.MULTILINE)
SOURCE_PATHS = frozenset((
    "go/cmd/reducer/ifa_group_retry_live_test.go",
    "go/internal/replay/offlinetier/repo_dependency_ifa_cleanup_live_test.go",
    "go/internal/replay/offlinetier/repo_dependency_ifa_concurrency_live_test.go",
    "go/internal/replay/offlinetier/repo_dependency_ifa_database_guard_live_test.go",
    "go/internal/replay/offlinetier/repo_dependency_ifa_process_death_live_test.go",
    "go/internal/replay/offlinetier/repo_dependency_ifa_quarantine_live_test.go",
    "go/internal/replay/offlinetier/repo_dependency_ifa_replay_coordinates_live_test.go",
))
PR_EXTRA_PATHS = frozenset((
    ".github/workflows/ifa-scheduled-proofs.yml",
    "specs/ci-pr-workflows.d/ifa-scheduled-proofs.yml",
    "go/go.mod",
    "go/go.sum",
    "scripts/ci/go-mod-download-retry.sh",
    "scripts/run-ifa-scheduled-proofs.sh",
    "scripts/lib/ifa_scheduled_events.py",
    "scripts/lib/ifa_scheduled_inventory.json",
    "scripts/lib/ifa_scheduled_manifest.py",
    "scripts/test-ifa-scheduled-manifest.py",
    "scripts/lib/ifa_scheduled_bundle.py",
    "scripts/test-ifa-scheduled-bundle.py",
    "scripts/lib/reducer_contention_events.py",
    "scripts/lib/verify-live-tests-ledger.py",
    "scripts/test-ifa-scheduled-events.py",
    "scripts/test-ifa-scheduled-ledger-policy.py",
    "scripts/verify-live-tests-ledger.sh",
    "scripts/test-verify-live-tests-ledger.sh",
    "specs/live-tests.v1.yaml",
    *(f"specs/live-tests.d/live-tests-0{number}.yaml" for number in range(1, 7)),
    "go/internal/replay/offlinetier/postgres_test.go",
    "go/internal/replay/offlinetier/delta_tier_live_test.go",
    "go/internal/replay/offlinetier/repo_dependency_proof_database_live_test.go",
    "go/internal/replay/offlinetier/repo_dependency_ifa_concurrency_fixture_test.go",
    "go/internal/replay/offlinetier/repo_dependency_ifa_concurrency_graph_test.go",
    "go/internal/replay/offlinetier/repo_dependency_ifa_concurrency_graph_validation_test.go",
))
ROOT_KEYS = frozenset({"name", "on", "concurrency", "permissions", "jobs"})
JOB_KEYS = frozenset({"if", "name", "runs-on", "timeout-minutes", "services", "steps"})
STEP_KEYS = {
    "Checkout repository": frozenset({"uses", "with"}),
    "Verify Ifá runner contract": frozenset({"run"}),
    "Set up Go": frozenset({"uses", "with"}),
    "Pre-warm Go modules": frozenset({"run"}),
    "Run tagged Ifá proofs": frozenset({"env", "run"}),
    "Upload Ifá proof events": frozenset({"if", "uses", "with"}),
}


def canonical_keys(text: str, indent: int, allowed: frozenset[str]) -> dict[str, str]:
    """Require literal, unique property keys in the owned workflow grammar."""
    entries: dict[str, str] = {}
    for line in text.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        depth = len(line) - len(line.lstrip(" "))
        if depth != indent:
            continue
        match = re.fullmatch(rf" {{{indent}}}([a-z][a-z-]*):(?: .*)?", line)
        if (
            match is None
            or match.group(1) not in allowed
            or match.group(1) in entries
        ):
            raise ValueError(f"non-fail-closed Ifá workflow key: {line}")
        entries[match.group(1)] = line
    return entries


def verify_workflow_structure(workflow: str) -> str:
    """Reject alternate YAML properties that can disable the owner job or steps."""
    root = canonical_keys(workflow, 0, ROOT_KEYS)
    if set(root) != ROOT_KEYS or root["jobs"] != "jobs:":
        raise ValueError("inactive Ifá owner job: noncanonical workflow root")
    jobs = workflow.split("\njobs:\n", 1)[1]
    job_headers = [
        line for line in jobs.splitlines()
        if line.startswith("  ") and not line.startswith("   ")
        and line.strip() and not line.lstrip().startswith("#")
    ]
    if job_headers != ["  ifa-scheduled:"]:
        raise ValueError("inactive Ifá owner job: noncanonical jobs")
    job = jobs.split("  ifa-scheduled:\n", 1)[1]
    keys = canonical_keys(job, 4, JOB_KEYS)
    if (
        set(keys) != JOB_KEYS
        or keys["if"] != "    if: ${{ github.event_name != 'pull_request' || contains(fromJSON(inputs.plan_jobs || '[]'), 'ifa-scheduled') }}"
        or keys["runs-on"] != "    runs-on: ubuntu-latest"
        or keys["timeout-minutes"] != "    timeout-minutes: 20"
        or keys["services"] != "    services:"
        or keys["steps"] != "    steps:"
    ):
        raise ValueError("inactive or non-fail-closed Ifá owner job")
    step_list = job.split("    steps:\n", 1)[1]
    header = re.compile(r"      - name: ([^\n]+)\n")
    for line in step_list.splitlines():
        if (line.startswith("      ") and not line.startswith("       ")
                and line.strip() and not line.lstrip().startswith("#")
                and header.fullmatch(line + "\n") is None):
            raise ValueError(f"non-fail-closed Ifá step header: {line}")
    matches = list(header.finditer(step_list))
    if [match.group(1) for match in matches] != list(STEP_KEYS):
        raise ValueError("Ifá owner job steps drifted")
    for index, match in enumerate(matches):
        end = (
            matches[index + 1].start()
            if index + 1 < len(matches)
            else len(step_list)
        )
        step = step_list[match.start():end]
        properties = canonical_keys(step, 8, STEP_KEYS[match.group(1)])
        if set(properties) != STEP_KEYS[match.group(1)]:
            raise ValueError(f"inactive or non-fail-closed Ifá step: {match.group(1)}")
        if match.group(1) == "Checkout repository" and step != (
            "      - name: Checkout repository\n"
            "        uses: actions/checkout@v5\n"
            "        with:\n"
            "          persist-credentials: false\n"
        ):
            raise ValueError("Ifá checkout must use the GitHub event revision")
    return job


def verify_workflow_route(workflow: str, fragment: str | None = None) -> None:
    """Require an exact PR path route to the same unconditional live job."""
    job = verify_workflow_structure(workflow)
    event_match = re.search(r"(?ms)^on:\n(.*?)(?=^[^ #][^\n]*:\s*(?:\n|$))", workflow)
    if not event_match:
        raise ValueError("missing active workflow event block")
    events = event_match.group(1)
    if not events.startswith(
        "  workflow_call:\n    inputs:\n      plan_jobs:\n"
        "        required: true\n        type: string\n"
        "      plan_gates:\n        required: true\n        type: string\n"
        "      plan_mode:\n        required: true\n        type: string\n"
        "  schedule:\n    - cron: '50 4 * * *'\n"
        "  workflow_dispatch:\n"
    ):
        raise ValueError("missing active Ifá daily cadence and callable event")
    event_names = re.findall(r"(?m)^  ([a-z_]+):", events)
    if event_names != ["workflow_call", "schedule", "workflow_dispatch"]:
        raise ValueError("workflow must have only call, schedule, and manual events")
    if fragment is None:
        fragment = (Path(__file__).resolve().parents[2] / FRAGMENT).read_text()
    active = [line for line in fragment.splitlines()
              if line.strip() and not line.lstrip().startswith("#")]
    try:
        start = active.index("  paths:")
        end = active.index("  jobs:")
    except ValueError as error:
        raise ValueError("Ifá pull_request path filter drifted") from error
    paths = [line.removeprefix("  - ") for line in active[start + 1:end]
             if line.startswith("  - ")]
    prefix = ["schema_version: 1", "workflows:",
              "- file: ifa-scheduled-proofs.yml", '  name: "If\\xE1 Scheduled Live Proofs"',
              "  synchronize: true", "  branches:", "  - main", "  paths:"]
    suffix = ["  jobs:", "  - ifa-scheduled", "  job_names:",
              '    ifa-scheduled: "If\\xE1 scheduled proof vs NornicDB and PostgreSQL"']
    if (active[:start + 1] != prefix or active[end:] != suffix
            or end <= start or len(paths) != end - start - 1
            or len(paths) != len(set(paths)) or set(paths) != SOURCE_PATHS | PR_EXTRA_PATHS):
        raise ValueError("Ifá pull_request path filter drifted")
    concurrency = (
        "concurrency:\n  group: ci-leaf-ifa-scheduled-proofs-${{ "
        "github.event_name == 'pull_request' && github.ref || 'scheduled' }}\n"
    )
    if concurrency not in workflow:
        raise ValueError("missing separate PR concurrency")
    contract_step = re.search(
        r"(?ms)^      - name: Verify Ifá runner contract\n(.*?)(?=^      - name:|\Z)", job
    )
    if (not contract_step or contract_step.group(1) !=
            "        run: |\n"
            "          python3 scripts/test-ifa-scheduled-events.py\n"
            "          python3 scripts/test-ifa-scheduled-manifest.py\n"
            "          python3 scripts/test-ifa-scheduled-bundle.py\n"
            "          python3 scripts/lib/ifa_scheduled_events.py\n"
            "          bash scripts/verify-live-tests-ledger.sh\n"):
        raise ValueError("Ifá contract verifier must run unconditionally")
    step_match = re.search(
        r"(?ms)^      - name: Run tagged Ifá proofs\n(.*?)(?=^      - name:|\Z)", job
    )
    if not step_match:
        raise ValueError("missing active Ifá live proof step")
    if (step_match.group(1) !=
            "        env:\n"
            "          POSTGRES_SERVICE_ID: ${{ job.services.postgres.id }}\n"
            "        run: |\n"
            "          set -euo pipefail\n"
            '          test -z "${GOFLAGS:-}"\n'
            '          test -z "${GOWORK:-}"\n'
            '          effective_config="$(go env GOFLAGS GOWORK)"\n'
            '          test -z "$effective_config"\n'
            '          proof_status="$(git status --porcelain --untracked-files=all)"\n'
            '          test -z "$proof_status"\n'
            "          bash scripts/run-ifa-scheduled-proofs.sh\n"):
        raise ValueError("Ifá live proof step must run unconditionally")
    upload = re.search(r"(?ms)^      - name: Upload Ifá proof events\n(.*)\Z", job)
    if (not upload or upload.group(1) !=
            "        if: ${{ always() }}\n"
            "        uses: actions/upload-artifact@v4\n"
            "        with:\n"
            "          name: ifa-scheduled-events\n"
            "          path: |\n"
            "            ${{ runner.temp }}/ifa-scheduled/*.jsonl\n"
            "            ${{ runner.temp }}/ifa-scheduled/manifest.json\n"
            "          if-no-files-found: error\n"):
        raise ValueError("Ifá event streams and manifest must upload together")
    if hashlib.sha256(workflow.encode("utf-8")).hexdigest() != WORKFLOW_SHA256:
        raise ValueError("Ifá workflow executable bytes differ from reviewed contract")


def load_inventory(path: Path = INVENTORY) -> dict[str, dict[str, object]]:
    """Load the exact seven-file Ifá proof inventory."""
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict) or set(value) != SOURCE_PATHS:
        raise ValueError("Ifá inventory must contain exactly seven reviewed files")
    return value


def require(text: str, token: str, context: str) -> None:
    """Reject removal of a required runner action."""
    if token not in text:
        raise ValueError(f"missing {context}: {token}")


def verify_inventory(
    root: Path,
    inventory: dict[str, dict[str, object]],
    *,
    runner_text: str | None = None,
    workflow_text: str | None = None,
) -> None:
    """Require exact tagged source, selectors, endpoints, and CI enrollment."""
    if set(inventory) != SOURCE_PATHS:
        raise ValueError("Ifá inventory path set drifted")
    if (NORNIC_DIGEST != "sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1" or
            POSTGRES_DIGEST != "sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873"):
        raise ValueError("Ifá manifest backend pins drifted")
    runner = runner_text if runner_text is not None else (root / RUNNER).read_text(encoding="utf-8")
    workflow = (
        workflow_text if workflow_text is not None
        else (root / WORKFLOW).read_text(encoding="utf-8")
    )
    active_lines = [line.strip() for line in runner.splitlines()
                    if line.strip() and not line.lstrip().startswith("#")]
    for token in (
        "set -euo pipefail",
        "sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1",
        "${primary}:17474:17687",
        "${secondary}:18474:18687",
        "ESHU_GRAPH_BACKEND=nornicdb",
        "ESHU_REPLAY_TIER_LIVE=1",
        "ESHU_REPO_DEPENDENCY_CONCURRENCY_PROVE_LIVE=1",
        "ESHU_REPO_DEPENDENCY_PROOF_HTTP_URL=http://127.0.0.1:17474",
        "ESHU_REPO_DEPENDENCY_PROOF_MISMATCH_HTTP_URL=http://127.0.0.1:18474",
        "ESHU_REPO_DEPENDENCY_QUARANTINE_PROOF_DSN=",
        "ESHU_NEO4J_URI=bolt://127.0.0.1:17687",
        "NEO4J_URI=\"${ESHU_NEO4J_URI}\"",
        "ESHU_NEO4J_DATABASE=nornic",
        "NEO4J_DATABASE=\"${ESHU_NEO4J_DATABASE}\"",
        "ESHU_NEO4J_USERNAME=neo4j",
        "NEO4J_USERNAME=\"${ESHU_NEO4J_USERNAME}\"",
        "ESHU_NEO4J_PASSWORD=change-me",
        "ifa_scheduled_manifest.py",
        "NEO4J_PASSWORD=\"${ESHU_NEO4J_PASSWORD}\"",
    ):
        require(runner, token, "runner contract")
    verify_workflow_route(workflow, (root / FRAGMENT).read_text(encoding="utf-8"))
    if hashlib.sha256(runner.encode("utf-8")).hexdigest() != RUNNER_SHA256:
        raise ValueError("Ifá runner executable bytes differ from reviewed contract")
    for line in (
        "set -euo pipefail",
        "export ESHU_GRAPH_BACKEND=nornicdb",
        "export ESHU_REPLAY_TIER_LIVE=1",
        "export ESHU_REPO_DEPENDENCY_CONCURRENCY_PROVE_LIVE=1",
        "export ESHU_REPO_DEPENDENCY_PROOF_HTTP_URL=http://127.0.0.1:17474",
        "export ESHU_REPO_DEPENDENCY_PROOF_MISMATCH_HTTP_URL=http://127.0.0.1:18474",
        "export ESHU_NEO4J_URI=bolt://127.0.0.1:17687",
        "export ESHU_NEO4J_DATABASE=nornic",
    ):
        if active_lines.count(line) != 1:
            raise ValueError(f"missing or duplicate active runner line: {line}")
    manifest_command = (
        'python3 scripts/lib/ifa_scheduled_manifest.py --primary "${primary}" '
        '--mismatch "${secondary}" --postgres "${POSTGRES_SERVICE_ID:?}" '
        '--output "${events_dir}/manifest.json"'
    )
    if active_lines.count(manifest_command) != 1:
        raise ValueError("missing active hosted manifest capture")
    bundle_command = 'python3 ../scripts/lib/ifa_scheduled_bundle.py --directory "${events_dir}"'
    if active_lines.count(bundle_command) != 1:
        raise ValueError("missing final manifest and event bundle verification")
    backend_loop = 'for entry in "${primary}:17474:17687" "${secondary}:18474:18687"; do'
    if active_lines.count(backend_loop) != 1:
        raise ValueError("runner must start two distinct pinned NornicDB instances")
    for token in (
        "50 4 * * *",
        "workflow_dispatch:",
        "actions/upload-artifact@v4",
        "ifa-scheduled/*.jsonl",
        "scripts/run-ifa-scheduled-proofs.sh",
        "python3 scripts/test-ifa-scheduled-events.py",
        "python3 scripts/lib/ifa_scheduled_events.py",
        "bash scripts/verify-live-tests-ledger.sh",
        "sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873",
        "POSTGRES_DB: eshu_ifa_proof",
    ):
        require(workflow, token, "workflow contract")
    for path, row in inventory.items():
        source = (root / path).read_text(encoding="utf-8")
        tag = "ifafaultinjection" if path.startswith("go/cmd/") else "ifarepodependencyproof"
        package = "github.com/eshu-hq/eshu/" + path.rsplit("/", 1)[0]
        tests = row.get("tests")
        if (row.get("tag") != tag or row.get("package") != package or
                not isinstance(tests, list) or tests != sorted(set(tests))):
            raise ValueError(f"invalid Ifá inventory row: {path}")
        if not re.search(r"(?m)^//go:build " + tag + r"$", source):
            raise ValueError(f"source build tag changed: {path}")
        if sorted(TEST_PATTERN.findall(source)) != tests:
            raise ValueError(f"source Test inventory changed: {path}")
    for package, tag, short in (
        (
            "github.com/eshu-hq/eshu/go/internal/replay/offlinetier",
            "ifarepodependencyproof",
            "repo",
        ),
        ("github.com/eshu-hq/eshu/go/cmd/reducer", "ifafaultinjection", "reducer"),
    ):
        names = sorted(
            name for row in inventory.values() if row["package"] == package
            for name in row["tests"]
        )
        if not names:
            raise ValueError(f"empty Ifá package inventory: {package}")
        selector = "^(" + "|".join(names) + ")$" if len(names) > 1 else "^" + names[0] + "$"
        command = (
            f"go test -json -race -p=1 -count=1 -timeout=6m -tags {tag} "
            f"./{package.split('/go/', 1)[1]} -run '{selector}' "
            f'| tee "${{events_dir}}/{short}.jsonl"'
        )
        if active_lines.count(command) != 1:
            raise ValueError(f"missing exact Go selector and fail-closed capture: {package}")
        verifier = (
            f'python3 ../scripts/lib/ifa_scheduled_events.py --events '
            f'"${{events_dir}}/{short}.jsonl" --package {package}'
        )
        if active_lines.count(verifier) != 1:
            raise ValueError(f"missing event verifier: {package}")


def main() -> int:
    """Check inventory, or check a package's captured event stream too."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--events", type=Path)
    parser.add_argument("--package")
    args = parser.parse_args()
    try:
        inventory = load_inventory()
        verify_inventory(args.repo_root, inventory)
        if args.events is None:
            print("Ifá scheduled inventory: seven files, eight tests verified")
            return 0
        if not args.package:
            parser.error("--package is required with --events")
        expected = {
            name for row in inventory.values() if row["package"] == args.package
            for name in row["tests"]
        }
        if not expected:
            raise ValueError(f"no enrolled tests for {args.package}")
        return 0 if verify_events(args.events, args.package, expected, sys.stderr) else 1
    except (OSError, ValueError, re.error) as error:
        print(error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
