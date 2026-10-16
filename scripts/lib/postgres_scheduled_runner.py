#!/usr/bin/env python3
"""Verify source enrollment and Go events for scheduled PostgreSQL proofs."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import sys

from reducer_contention_events import verify_events as verify_go_events
from postgres_scheduled_fragments import load_inventory as load_fragments, inventory_digest
from postgres_scheduled_manifest import build_manifest as build_source_bound_manifest
from postgres_scheduled_workflow import (
    verify_canonical_workflow, verify_trigger as verify_workflow_trigger,
)


INVENTORY = Path(__file__).with_name("postgres_scheduled_inventory.json")
WORKFLOW = Path(".github/workflows/postgres-scheduled-live.yml")
WORKFLOW_SHA256 = "1e52574d4193bba24a524a5b12c9cc7f663e14a8887033084ba8ba12c3a0b166"
TEST_PATTERN = re.compile(r"^func (Test\w+)\(", re.MULTILINE)
PACKAGES = {
    "crossplane": "github.com/eshu-hq/eshu/go/internal/storage/postgres/crossplane",
    "infra_inventory": "github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory",
    "disposable_gate": "github.com/eshu-hq/eshu/go/cmd/golden-corpus-gate",
    "disposable_query": "github.com/eshu-hq/eshu/go/internal/query",
    "disposable_storage": "github.com/eshu-hq/eshu/go/internal/storage/postgres",
    "ack_family": "github.com/eshu-hq/eshu/go/internal/storage/postgres",
    **{group: "github.com/eshu-hq/eshu/go/internal/storage/postgres"
       for group in ("claim", "liveness", "fanout", "search", "readiness")},
}
DSNS = {
    "crossplane": "ESHU_CROSSPLANE_REDRIVE_PROOF_DSN",
    "infra_inventory": "ESHU_POSTGRES_DSN",
    "ack_family": "ESHU_POSTGRES_TEST_DSN",
    "claim": "ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN",
    "liveness": "ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN",
    "fanout": "ESHU_POSTGRES_DSN",
    "search": "ESHU_POSTGRES_DSN",
    "readiness": "ESHU_REDUCER_FAIRNESS_PROOF_DSN",
}
DATABASES = {
    "crossplane": "eshu_crossplane_scheduled",
    "infra_inventory": "eshu_inventory_scheduled",
    "ack_family": "eshu_ack_scheduled",
    **{group: f"eshu_{group}_scheduled"
       for group in ("claim", "liveness", "fanout", "search", "readiness")},
}

ADMIN_ENVS = {
    "disposable_gate": ("ESHU_TEST_DRAIN_RESIDUAL_POSTGRES",),
    "disposable_query": (
        "ESHU_TEST_CONTENT_INDEX_POSTGRES",
        "ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES",
    ),
    "disposable_storage": ("ESHU_TEST_CONTENT_INDEX_POSTGRES",),
}
IMAGE = (
    "ghcr.io/eshu-hq/ci-postgres-alpine@sha256:"
    "77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873"
)
PR_PATHS = {
    ".github/workflows/postgres-scheduled-live.yml",
    "go/go.mod",
    "go/go.sum",
    "go/internal/testutil/postgresproof/**",
    "go/internal/query/testdata/**",
    "go/internal/query/*fixture*test.go",
    "go/internal/storage/postgres/testdata/**",
    "go/internal/storage/postgres/*fixture*test.go",
    "go/internal/storage/postgres/ack_supersession_assertions_test.go",
    "go/internal/storage/postgres/projector_queue_supersession_live_test.go",
    "go/internal/storage/postgres/queue_observer_test.go",
    "go/internal/storage/postgres/reducer_queue_cross_scope_readiness_gate_coverage_test.go",
    "go/internal/storage/postgres/reducer_queue_domain_fairness_test.go",
    "go/internal/storage/postgres/reducer_queue_supersede_readiness_test.go",
    "go/internal/storage/postgres/crossplane/testdata/**",
    "go/internal/storage/postgres/infra/inventory/testdata/**",
    "go/cmd/golden-corpus-gate/testdata/**",
    "scripts/ci/go-mod-download-retry.sh",
    "scripts/lib/postgres_scheduled_inventory.json",
    "scripts/lib/postgres_scheduled_inventory.d/**",
    "scripts/lib/postgres_scheduled_fragments.py",
    "scripts/test-postgres-scheduled-fragments.py",
    "scripts/lib/postgres_scheduled_manifest.py",
    "scripts/lib/postgres_scheduled_workflow.py",
    "scripts/lib/postgres_scheduled_runner.py",
    "scripts/lib/reducer_contention_events.py",
    "scripts/lib/live_tests_registry.py",
    "scripts/lib/verify-live-tests-ledger.py",
    "scripts/test-postgres-scheduled-runner.py",
    "scripts/test-postgres-scheduled-ledger-policy.py",
    "scripts/test-verify-live-tests-ledger.sh",
    "scripts/verify-live-tests-ledger.sh",
    "specs/live-tests.v1.yaml",
    "specs/live-tests.d/**",
    "specs/ci-gates.d/gates-09.yaml",
    "specs/ci-gates.v1.yaml",
}


def verify_trigger(workflow: str, inventory: dict[str, dict[str, object]]) -> None:
    """Verify the canonical scheduled and narrow PR trigger."""
    verify_canonical_workflow(workflow)
    verify_workflow_trigger(workflow, inventory, PR_PATHS)


def load_inventory(path: Path = INVENTORY) -> dict[str, dict[str, object]]:
    """Load the reviewed exact file and test inventory."""
    return load_fragments(path)


def verify_inventory(root: Path, inventory: dict[str, dict[str, object]]) -> None:
    """Reject source, schedule, backend, selector, or event-check drift."""
    workflow = (root / WORKFLOW).read_text(encoding="utf-8")
    if "--local-simulation" in workflow or "--allow-local-simulation" in workflow:
        raise ValueError("local simulation cannot appear in hosted workflow")
    verify_trigger(workflow, inventory)
    if IMAGE not in workflow or "postgres:latest" in workflow:
        raise ValueError("pinned PostgreSQL backend missing")
    if workflow.count(IMAGE) != 1:
        raise ValueError("pinned PostgreSQL service must appear once")
    steps = re.split(r"(?m)^      - name:", workflow)
    def named_step(name: str) -> tuple[int, str]:
        """Return the only active step with the exact reviewed name."""
        matches = [
            (index, step) for index, step in enumerate(steps)
            if step.startswith(f" {name}\n")
        ]
        if len(matches) != 1:
            raise ValueError(f"missing or duplicate hosted proof step: {name}")
        return matches[0]

    bootstrap = [
        step for step in steps
        if step.startswith(" Check exact enrollment and negative cases\n")
    ]
    if len(bootstrap) != 1:
        raise ValueError("scheduled runner harness step missing")
    for command in (
        "python3 scripts/test-postgres-scheduled-runner.py",
        "python3 scripts/test-postgres-scheduled-fragments.py",
        "python3 scripts/test-postgres-scheduled-ledger-policy.py",
        "python3 scripts/lib/postgres_scheduled_runner.py",
    ):
        if len(re.findall(r"(?m)^          " + re.escape(command) + r"$", bootstrap[0])) != 1:
            raise ValueError("scheduled runner harness command missing")
    if re.search(r"(?m)^\s*if:", bootstrap[0]):
        raise ValueError("conditional scheduled runner harness step")
    manifest_index, manifest_step = named_step("Record hosted proof manifest")
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
        if group == "ack_family":
            timeout = "10m"
        elif group in {"crossplane", "infra_inventory"}:
            timeout = "7m"
        else:
            timeout = "6m"
        pattern = re.compile(
            r"(?m)^\s*go test -json -race -p=1 -count=1 -timeout="
            + timeout + " "
            + re.escape(package_path)
            + r" -run '([^']+)' \| tee \"\$\{RUNNER_TEMP\}/"
            + re.escape(group)
            + r"\.jsonl\"$"
        )
        selected_steps = [step for step in steps if pattern.search(step)]
        if len(selected_steps) != 1:
            raise ValueError(f"missing or duplicate selector for {group}")
        step = selected_steps[0]
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
            if group == "search" and step.count('ESHU_SEARCH_INDEX_PARTITION_LIVE: "1"') != 1:
                raise ValueError("search partition opt-in missing")
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
        if step.count('          proof_status="$(git -C .. status --porcelain --untracked-files=all -- go scripts .github/workflows)"') != 2 or step.count('          test -z "$proof_status"') != 2:
            raise ValueError(f"reviewed source guard missing for {group}")
        if re.search(r"(?m)^\s*if:", step):
            raise ValueError(f"conditional proof step for {group}")
    if workflow.count("set -euo pipefail") != len(PACKAGES) + 1:
        raise ValueError("every scheduled package step requires fail-closed shell")
    for group, database in DATABASES.items():
        label = {"crossplane": "Crossplane", "infra_inventory": "inventory",
                 "ack_family": "ACK"}.get(group, group)
        create_index, create_step = named_step(f"Create fresh {label} database")
        expected_create = (
            '        run: docker exec "${{ job.services.postgres.id }}" '
            'psql -v ON_ERROR_STOP=1 -U eshu -d postgres '
            f"-c 'CREATE DATABASE {database}'"
        )
        dsn = f"{DSNS[group]}: postgres://eshu:change-me@localhost:15432/{database}?sslmode=disable"
        if (create_step.splitlines().count(expected_create) != 1
                or workflow.count(f"-c 'CREATE DATABASE {database}'") != 1
                or workflow.count(dsn) != 1):
            raise ValueError(f"fresh database and DSN mismatch for {group}")
        run_name = {"infra_inventory": "Run infra inventory proofs",
                    "ack_family": "Run ACK and completion proofs"}.get(
                        group, f"Run {label} proofs")
        run_index, _ = named_step(run_name)
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
    bundle_index, bundle_step = named_step("Verify hosted proof bundle")
    expected_bundle = (
        '        run: python3 scripts/lib/postgres_scheduled_runner.py '
        '--verify-proof-bundle "${RUNNER_TEMP}"'
    )
    if expected_bundle not in bundle_step.splitlines():
        raise ValueError("hosted proof bundle verification missing")
    if re.search(r"(?m)^\s*if:", bundle_step):
        raise ValueError("conditional hosted proof bundle step")
    artifact_index, artifact_step = named_step("Retain hosted proof bundle")
    artifact_lines = {
        "        if: always()",
        "        uses: actions/upload-artifact@v4",
        "          name: postgres-scheduled-proof-${{ github.run_id }}-"
        "${{ github.run_attempt }}",
        "          if-no-files-found: error",
        "          retention-days: 14",
    } | {
        f"            ${{{{ runner.temp }}}}/{name}"
        for name in ("manifest.json", *(f"{group}.jsonl" for group in PACKAGES))
    }
    if not artifact_lines.issubset(set(artifact_step.splitlines())):
        raise ValueError("hosted proof artifact retention missing")
    uploaded = re.search(
        r"(?m)^          path: \|\n((?:            [^\n]+\n)+)",
        artifact_step,
    )
    expected_uploads = [
        f"            ${{{{ runner.temp }}}}/{name}"
        for name in ("manifest.json", *(f"{group}.jsonl" for group in PACKAGES))
    ]
    if not uploaded or uploaded.group(1).splitlines() != expected_uploads:
        raise ValueError("hosted proof artifact paths differ from exact bundle")
    if not (steps.index(bootstrap[0]) < manifest_index < bundle_index < artifact_index):
        raise ValueError("hosted proof step order changed")
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
        ("run_attempt", "GITHUB_RUN_ATTEMPT", "run attempt"),
        ("checkout_sha", "GITHUB_SHA", "checkout SHA"),
        ("event_sha", "GITHUB_SHA", "event SHA"),
    ):
        if str(manifest.get(field)) != os.environ.get(variable):
            raise ValueError(f"hosted {label} differs from current GitHub run")
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
