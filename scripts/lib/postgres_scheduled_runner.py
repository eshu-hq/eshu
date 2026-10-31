#!/usr/bin/env python3
"""Verify source enrollment and Go events for scheduled PostgreSQL proofs."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import sys

from postgres_scheduled_contract import (
    ADMIN_ENVS, DATABASES, DSNS, IMAGE, PACKAGES, PR_PATHS, STORAGE_GROUPS,
)
from postgres_scheduled_actions import (
    BULK_ACTION, STORAGE_ACTION, expand_action,
    expand_actions as expand_reviewed_actions,
)
from postgres_scheduled_artifacts import assemble_proof_bundle, verify_artifact_sources
from reducer_contention_events import verify_events as verify_go_events
from postgres_scheduled_fragments import load_inventory as load_fragments, inventory_digest
from postgres_scheduled_manifest import build_manifest as build_source_bound_manifest
from postgres_scheduled_matrix import (
    root_storage_groups, verify_aggregate_bundle, verify_leg_uploads,
    verify_root_storage_builds,
)
from postgres_scheduled_workflow import (
    JOB_ORDER, LEG_JOBS, SERVICE_JOBS, job_blocks, named_step,
    verify_canonical_workflow, verify_trigger as verify_workflow_trigger,
)


INVENTORY = Path(__file__).with_name("postgres_scheduled_inventory.json")
WORKFLOW = Path(".github/workflows/postgres-scheduled-live.yml")
FRAGMENT = Path("specs/ci-pr-workflows.d/postgres-scheduled-live.yml")
WORKFLOW_SHA256 = "3b4804b1ee185b3a397d052ecf768e75e556ae2240e92a3eb37f518fa100eb0f"
BULK_ACTION_SHA256 = "937bb489c42ec113d8886a594a10c56fae2872610ccf667ff72c0a25203e848b"
STORAGE_ACTION_SHA256 = "53327aaa04e313ce57aae169e84ef977773f07afdb9df899ffd1e9ea01c5370a"
TEST_PATTERN = re.compile(r"^func (Test\w+)\(", re.MULTILINE)
GROUP_LABELS = {
    "crossplane": "Crossplane",
    "infra_inventory": "inventory",
    "ack_family": "ACK",
    "freshness_links_scale": "freshness scale",
    "freshness_links_a": "freshness chunk A",
    "freshness_links_b": "freshness chunk B",
}
GROUP_TIMEOUTS = {
    "secret_lines": "8m",
    "ack_family": "10m",
    "crossplane": "7m",
    "infra_inventory": "7m",
    "freshness_links_scale": "30m",
    "freshness_links_a": "10m",
    "freshness_links_b": "10m",
}



def expand_bulk_action(workflow: str, action: str) -> str:
    """Expand only the bulk composite against its reviewed digest."""
    return expand_action(workflow, action, "bulk", BULK_ACTION_SHA256)


def expand_actions(workflow: str, root: Path) -> str:
    """Expand both reviewed composites against the digests pinned here."""
    return expand_reviewed_actions(workflow, root, {
        "bulk": BULK_ACTION_SHA256, "storage": STORAGE_ACTION_SHA256,
    })


def verify_trigger(workflow: str, inventory: dict[str, dict[str, object]], fragment: str) -> None:
    """Verify the canonical scheduled and narrow PR trigger."""
    verify_canonical_workflow(workflow)
    verify_workflow_trigger(workflow, inventory, PR_PATHS, fragment)


def load_inventory(path: Path = INVENTORY) -> dict[str, dict[str, object]]:
    """Load the reviewed exact file and test inventory."""
    return load_fragments(path)


def verify_inventory(root: Path, inventory: dict[str, dict[str, object]]) -> None:
    """Reject source, schedule, backend, selector, or event-check drift."""
    workflow = (root / WORKFLOW).read_text(encoding="utf-8")
    if "--local-simulation" in workflow or "--allow-local-simulation" in workflow:
        raise ValueError("local simulation cannot appear in hosted workflow")
    expanded = expand_actions(workflow, root)
    verify_trigger(expanded, inventory, (root / FRAGMENT).read_text(encoding="utf-8"))
    if IMAGE not in expanded or "postgres:latest" in expanded:
        raise ValueError("pinned PostgreSQL backend missing")
    blocks = job_blocks(expanded)
    if list(blocks) != list(JOB_ORDER):
        raise ValueError("hosted proof legs differ from reviewed matrix")
    for job in SERVICE_JOBS:
        if blocks[job].count(IMAGE) != 1:
            raise ValueError(f"pinned PostgreSQL service must appear once: {job}")
    if IMAGE in blocks["scheduled-aggregate"]:
        raise ValueError("aggregate job must not run a database service")
    setup_steps = re.split(r"(?m)^      - name:", blocks["scheduled-setup"])
    aggregate_steps = re.split(r"(?m)^      - name:", blocks["scheduled-aggregate"])

    bootstrap = [
        step for step in setup_steps
        if step.startswith(" Check exact enrollment and negative cases\n")
    ]
    if len(bootstrap) != 1:
        raise ValueError("scheduled runner harness step missing")
    for command in (
        "python3 scripts/test-postgres-scheduled-runner.py",
        "python3 scripts/test-postgres-scheduled-rerun.py",
        "python3 scripts/test-postgres-scheduled-matrix.py",
        "python3 scripts/test-postgres-scheduled-fragments.py",
        "python3 scripts/test-postgres-scheduled-ledger-policy.py",
        "python3 scripts/lib/postgres_scheduled_runner.py",
    ):
        if len(re.findall(r"(?m)^          " + re.escape(command) + r"$", bootstrap[0])) != 1:
            raise ValueError("scheduled runner harness command missing")
    if re.search(r"(?m)^\s*if:", bootstrap[0]):
        raise ValueError("conditional scheduled runner harness step")
    manifest_index, manifest_step = named_step(setup_steps, "Record hosted proof manifest")
    manifest_lines = {
        "          POSTGRES_CONTAINER: ${{ job.services.postgres.id }}",
        "          PR_HEAD_SHA: ${{ github.event.pull_request.head.sha }}",
        "          set -euo pipefail",
        "          python3 scripts/lib/postgres_scheduled_runner.py \\",
        '          checkout_sha="$(git rev-parse HEAD)"',
        '          reviewed_head_sha="${PR_HEAD_SHA:-$checkout_sha}"',
        "          postgres_identity=\"$(docker exec \"$POSTGRES_CONTAINER\" "
        "psql -v ON_ERROR_STOP=1 -At -U eshu -d postgres -c 'SELECT version()')\"",
        '            --write-manifest "${RUNNER_TEMP}/manifest.json" \\',
        '            --event "${GITHUB_EVENT_NAME}" \\',
        '            --run-id "${GITHUB_RUN_ID}" \\',
        '            --run-attempt "${GITHUB_RUN_ATTEMPT}" \\',
        "            --run-url \"https://github.com/${GITHUB_REPOSITORY}/actions/"
        "runs/${GITHUB_RUN_ID}/attempts/${GITHUB_RUN_ATTEMPT}\" \\",
        '            --reviewed-head-sha "$reviewed_head_sha" \\',
        '            --checkout-sha "$checkout_sha" \\',
        '            --event-sha "${GITHUB_SHA}" \\',
        '            --backend-identity "$postgres_identity"',
    }
    if not manifest_lines.issubset(set(manifest_step.splitlines())):
        raise ValueError("hosted proof manifest command or identity missing")
    if re.search(r"(?m)^\s*if:", manifest_step):
        raise ValueError("conditional hosted proof manifest step")
    for group, package in PACKAGES.items():
        package_path = "./" + package.split("/go/", 1)[1]
        timeout = GROUP_TIMEOUTS.get(group, "6m")
        arguments = (
            " -args -containerimage-bootstrap=1"
            " -containerimage-database=eshu_containerimage_scheduled"
            if group == "containerimage" else ""
        )
        pattern = re.compile(
            r"(?m)^\s*go test -json -race -p=1 -count=1 -timeout="
            + timeout + " "
            + re.escape(package_path)
            + r" -run '([^']+)'" + re.escape(arguments)
            + r" \| tee \"\$\{RUNNER_TEMP\}/"
            + re.escape(group)
            + r"\.jsonl\"$"
        )
        leg_steps = [
            step
            for job in LEG_JOBS
            for step in re.split(r"(?m)^      - name:", blocks[job])
            if pattern.search(step)
        ]
        if len(leg_steps) != 1:
            raise ValueError(f"missing or duplicate selector for {group}")
        step = leg_steps[0]
        selected = pattern.search(step).group(1)
        expected = sorted(
            name for row in inventory.values() if row.get("group") == group
            for name in row.get("tests", [])
        )
        if selected != "^(" + "|".join(expected) + ")$":
            raise ValueError(f"selector does not equal exact inventory for {group}")
        if group in DSNS:
            if step.count(f"{DSNS[group]}: postgres://") != 1:
                raise ValueError(f"missing or duplicate DSN for {group}")
            dsn = (
                "postgres://eshu:change-me@localhost:15432/"
                f"{DATABASES[group]}?sslmode=disable"
            )
            if step.count(f"{DSNS[group]}: {dsn}") != 1:
                raise ValueError(f"wrong fresh database for {group}")
            extras = {
                "ack_family": ("ESHU_POSTGRES_DSN",),
                "claim": ("ESHU_POSTGRES_DSN", "ESHU_REDUCER_FAIRNESS_PROOF_DSN"),
                "fanout": ("ESHU_REDUCER_FAIRNESS_PROOF_DSN",),
                "readiness": ("ESHU_POSTGRES_DSN",),
            }
            for name in extras.get(group, ()):
                if step.count(f"{name}: {dsn}") != 1:
                    raise ValueError(f"secondary DSN differs for {group}")
            if group == "admin" and step.count('ESHU_ADMIN_REPLAY_FENCE_LIVE: "1"') != 1:
                raise ValueError("admin replay fence opt-in missing")
            if group == "search" and step.count('ESHU_SEARCH_INDEX_PARTITION_LIVE: "1"') != 1:
                raise ValueError("search partition opt-in missing")
            if group == "freshness_links_a":
                if step.count('          ESHU_7127_W11_COST: "1"') != 1:
                    raise ValueError("missing freshness cost opt-in for chunk A")
            if group == "freshness_links_scale":
                for line in (
                    'ESHU_7127_W11_COST: "1"',
                    "ESHU_RETENTION_TIMING_DSN: postgres://eshu:change-me@localhost:15432/"
                    "eshu_freshness_scheduled?sslmode=disable",
                    'ESHU_RETENTION_TIMING_MODE: "run"',
                    'ESHU_RETENTION_TIMING_FIXTURES: "link"',
                    'ESHU_RETENTION_TIMING_FIXTURE: "link"',
                    "ESHU_CHANGED_SINCE_LINK_SCALE_DSN: postgres://eshu:change-me@localhost:15432/"
                    "eshu_freshness_scale?sslmode=disable",
                    'ESHU_CHANGED_SINCE_LINK_SCALE_BASE_REF: "HEAD"',
                    'ESHU_CHANGED_SINCE_LINK_SCALE_ROUNDS: "1"',
                    'ESHU_CHANGED_SINCE_LINK_SCALE_VALID_ROUNDS: "1"',
                    'ESHU_CHANGED_SINCE_LINK_SCALE_DEADLINE: "30m"',
                    'ESHU_CHANGED_SINCE_LINK_SCALE_PROOF: "p3"',
                    "ESHU_RETENTION_TIMING_MODE=build go test -race -p=1 -count=1 "
                    "-timeout=10m ./internal/storage/postgres/freshness/links "
                    "-run '^TestRetentionTimingP8$'",
                ):
                    if step.count(f"          {line}") != 1:
                        raise ValueError(f"missing freshness links fixture pin: {line}")
                if step.count("internal/storage/postgres/freshness/links/testdata/7127-link-writer-fixture.sql") != 1:
                    raise ValueError("missing freshness links scale fixture load")
        else:
            for prefix in ADMIN_ENVS[group]:
                dsn = (
                    f"{prefix}_DSN: postgres://eshu:change-me@localhost:15432/"
                    "postgres?sslmode=disable"
                )
                if step.count(dsn) != 1 or step.count(f'{prefix}_DISPOSABLE: "1"') != 1:
                    raise ValueError(f"missing admin DSN or disposable opt-in for {group}")
        verifier = (
            f"python3 ../scripts/lib/postgres_scheduled_runner.py --events "
            f'"${{RUNNER_TEMP}}/{group}.jsonl" --group {group}'
        )
        if len(re.findall(r"(?m)^          " + re.escape(verifier) + r"$", step)) != 1:
            raise ValueError(f"missing event verifier for {group}")
        if len(re.findall(r"(?m)^          set -euo pipefail$", step)) != 1:
            raise ValueError(f"missing fail-closed shell for {group}")
        if (step.count('          effective_config="$(go env GOFLAGS GOWORK)"') != 2 or
                step.count('          test -z "$effective_config"') != 2 or
                step.count('          proof_status="$(git -C .. status --porcelain --untracked-files=all)"') != 2 or
                step.count('          test -z "$proof_status"') != 2):
            raise ValueError(f"reviewed source guard missing for {group}")
        if re.search(r"(?m)^\s*if:", step):
            raise ValueError(f"conditional proof step for {group}")
    if expanded.count("set -euo pipefail") != len(PACKAGES) + 1:
        raise ValueError("every scheduled package step requires fail-closed shell")
    all_steps = re.split(r"(?m)^      - name:", expanded)
    for group, database in DATABASES.items():
        label = GROUP_LABELS.get(group, group)
        create_index, create_step = named_step(all_steps, f"Create fresh {label} database")
        expected_create = (
            '        run: docker exec "${{ job.services.postgres.id }}" '
            'psql -v ON_ERROR_STOP=1 -U eshu -d postgres '
            f"-c 'CREATE DATABASE {database}'"
        )
        dsn = f"{DSNS[group]}: postgres://eshu:change-me@localhost:15432/{database}?sslmode=disable"
        if (create_step.splitlines().count(expected_create) != 1
                or expanded.count(f"-c 'CREATE DATABASE {database}'") != 1
                or expanded.count(dsn) != 1):
            raise ValueError(f"fresh database and DSN mismatch for {group}")
        run_name = {"infra_inventory": "Run infra inventory proofs",
                    "ack_family": "Run ACK and completion proofs"}.get(
                        group, f"Run {label} proofs")
        run_index, _ = named_step(all_steps, run_name)
        if create_index >= run_index:
            raise ValueError(f"fresh database is not created before {group}")

    for relative, row in inventory.items():
        group, package, tests = row.get("group"), row.get("package"), row.get("tests")
        if group not in PACKAGES or package != PACKAGES[group]:
            raise ValueError(f"wrong inventory group or package: {relative}")
        if not isinstance(tests, list) or not tests or tests != sorted(set(tests)):
            raise ValueError(f"invalid inventory tests: {relative}")
        if package != "github.com/eshu-hq/eshu/" + relative.rsplit("/", 1)[0]:
            raise ValueError(f"wrong source package: {relative}")
        source = (root / relative).read_text(encoding="utf-8")
        actual = sorted(TEST_PATTERN.findall(source))
        if actual != tests:
            raise ValueError(f"source Test inventory changed: {relative}")
    verify_leg_uploads(blocks)
    verify_root_storage_builds(blocks)
    verify_aggregate_bundle(aggregate_steps)
    if hashlib.sha256(workflow.encode("utf-8")).hexdigest() != WORKFLOW_SHA256:
        raise ValueError("hosted workflow executable bytes differ from reviewed contract")


def verify_events(path: Path, package: str, expected: set[str]) -> bool:
    """Require each enrolled test and the package to run and pass once."""
    return verify_go_events(path, package, expected, sys.stderr)


def build_manifest(
    inventory: dict[str, dict[str, object]], event: str, run_id: str,
    run_attempt: str, run_url: str, reviewed_head_sha: str,
    checkout_sha: str, backend_identity: str, event_sha: str | None = None,
    source_root: Path | None = None,
    local_simulation: bool = False,
) -> dict[str, object]:
    """Describe one hosted attempt using the enrolled source contract."""
    return build_source_bound_manifest(
        inventory, event, run_id, run_attempt, run_url,
        reviewed_head_sha, checkout_sha, backend_identity,
        event_sha, source_root, local_simulation,
        inventory_path=INVENTORY, packages=PACKAGES, image=IMAGE,
    )


def verify_hosted_environment(manifest: dict[str, object]) -> None:
    """Bind hosted proof metadata to the current GitHub Actions attempt."""
    if os.environ.get("GITHUB_ACTIONS") != "true":
        return
    if os.environ.get("GITHUB_REPOSITORY") != "eshu-hq/eshu":
        raise ValueError("hosted repository differs from proof manifest")
    for field, variable, label in (
        ("event", "GITHUB_EVENT_NAME", "event"),
        ("run_id", "GITHUB_RUN_ID", "run ID"),
        ("checkout_sha", "GITHUB_SHA", "checkout SHA"),
        ("event_sha", "GITHUB_SHA", "event SHA"),
    ):
        if str(manifest.get(field)) != os.environ.get(variable):
            raise ValueError(f"hosted {label} differs from current GitHub run")
    current_attempt = os.environ.get("GITHUB_RUN_ATTEMPT", "")
    manifest_attempt = manifest.get("run_attempt")
    if (not current_attempt.isdecimal() or int(current_attempt) < 1
            or not isinstance(manifest_attempt, int)
            or manifest_attempt < 1 or manifest_attempt > int(current_attempt)):
        raise ValueError("hosted run attempt differs from current GitHub run")
    if manifest.get("event") == "pull_request":
        event_path = os.environ.get("GITHUB_EVENT_PATH")
        if not event_path:
            raise ValueError("hosted pull request event path missing")
        event = json.loads(Path(event_path).read_text(encoding="utf-8"))
        pull_request = event.get("pull_request") if isinstance(event, dict) else None
        head = pull_request.get("head") if isinstance(pull_request, dict) else None
        expected_head = head.get("sha") if isinstance(head, dict) else None
        if manifest.get("reviewed_head_sha") != expected_head:
            raise ValueError("hosted reviewed head SHA differs from pull request")


def verify_proof_bundle(
    bundle: Path, inventory: dict[str, dict[str, object]],
    source_root: Path | None = None,
    allow_local_simulation: bool = False,
) -> None:
    """Require a matching manifest and every strict Go event stream."""
    manifest_path = bundle / "manifest.json"
    if not manifest_path.is_file():
        raise ValueError("missing hosted proof manifest")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if not isinstance(manifest, dict):
        raise ValueError("invalid hosted proof manifest")
    origin = manifest.get("proof_origin")
    if origin not in {"hosted", "local_simulation"}:
        raise ValueError("invalid proof origin")
    if origin == "local_simulation" and not allow_local_simulation:
        raise ValueError("local simulation is not hosted proof")
    if allow_local_simulation and os.environ.get("GITHUB_ACTIONS") == "true":
        raise ValueError("local simulation cannot pass in GitHub Actions")
    expected = build_manifest(
        inventory=inventory,
        event=manifest.get("event"),
        run_id=manifest.get("run_id"),
        run_attempt=str(manifest.get("run_attempt", "")),
        run_url=manifest.get("run_url"),
        reviewed_head_sha=manifest.get("reviewed_head_sha"),
        checkout_sha=manifest.get("checkout_sha"),
        event_sha=manifest.get("event_sha"),
        backend_identity=manifest.get("postgres_identity"),
        source_root=source_root,
        local_simulation=origin == "local_simulation",
    )
    if manifest.get("inventory_sha256") != expected["inventory_sha256"]:
        raise ValueError("hosted proof inventory checksum mismatch")
    if manifest.get("source_sha256") != expected["source_sha256"]:
        raise ValueError("hosted proof source checksum mismatch")
    if manifest != expected:
        raise ValueError("hosted proof manifest differs from expected run")
    verify_hosted_environment(manifest)
    if os.environ.get("GITHUB_ACTIONS") == "true":
        verify_artifact_sources(
            bundle, manifest, int(os.environ["GITHUB_RUN_ATTEMPT"]))
    for group, package in PACKAGES.items():
        path = bundle / f"{group}.jsonl"
        if not path.is_file():
            raise ValueError(f"missing hosted event stream: {group}")
        tests = {
            test for row in inventory.values() if row["group"] == group
            for test in row["tests"]
        }
        if not verify_events(path, package, tests):
            raise ValueError(f"invalid hosted event stream: {group}")


def main() -> int:
    """Check the committed inventory, then optionally one Go event stream."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--events", type=Path)
    parser.add_argument("--group", choices=tuple(PACKAGES))
    parser.add_argument("--write-manifest", type=Path)
    parser.add_argument("--verify-proof-bundle", type=Path)
    parser.add_argument("--assemble-proof-streams", type=Path)
    parser.add_argument("--output-proof-bundle", type=Path)
    parser.add_argument("--event")
    parser.add_argument("--run-id")
    parser.add_argument("--run-attempt")
    parser.add_argument("--run-url")
    parser.add_argument("--reviewed-head-sha")
    parser.add_argument("--checkout-sha")
    parser.add_argument("--event-sha")
    parser.add_argument("--backend-identity")
    parser.add_argument("--local-simulation", action="store_true")
    parser.add_argument("--allow-local-simulation", action="store_true")
    arguments = parser.parse_args()
    try:
        inventory = load_inventory()
        verify_inventory(arguments.repo_root, inventory)
        if arguments.write_manifest:
            if os.environ.get("GITHUB_ACTIONS") == "true":
                if arguments.local_simulation:
                    raise ValueError("local simulation cannot run in GitHub Actions")
            elif not arguments.local_simulation:
                raise ValueError("local manifest writer requires --local-simulation")
            manifest = build_manifest(
                inventory, arguments.event, arguments.run_id,
                arguments.run_attempt, arguments.run_url,
                arguments.reviewed_head_sha, arguments.checkout_sha,
                arguments.backend_identity, arguments.event_sha,
                arguments.repo_root, arguments.local_simulation,
            )
            arguments.write_manifest.write_text(
                json.dumps(manifest, sort_keys=True, indent=2) + "\n",
                encoding="utf-8",
            )
            return 0
        if arguments.verify_proof_bundle:
            verify_proof_bundle(
                arguments.verify_proof_bundle, inventory, arguments.repo_root,
                arguments.allow_local_simulation,
            )
            label = (
                "local simulated" if arguments.allow_local_simulation else "hosted"
            )
            print(f"scheduled PostgreSQL {label} proof bundle verified")
            return 0
        if arguments.assemble_proof_streams:
            if not arguments.output_proof_bundle:
                parser.error("--output-proof-bundle is required with --assemble-proof-streams")
            assemble_proof_bundle(
                arguments.assemble_proof_streams, arguments.output_proof_bundle,
                os.environ.get("GITHUB_RUN_ID", ""),
                int(os.environ.get("GITHUB_RUN_ATTEMPT", "0")),
            )
            return 0
        if arguments.events is None:
            print(f"scheduled PostgreSQL inventory: {len(inventory)} files verified")
            return 0
        if arguments.group is None:
            parser.error("--group is required with --events")
        expected = {
            name for row in inventory.values() if row["group"] == arguments.group
            for name in row["tests"]
        }
        return 0 if verify_events(arguments.events, PACKAGES[arguments.group], expected) else 1
    except (OSError, ValueError, re.error, json.JSONDecodeError) as error:
        print(error, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
