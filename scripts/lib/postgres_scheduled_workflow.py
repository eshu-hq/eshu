#!/usr/bin/env python3
"""Restrict the scheduled PostgreSQL workflow to reviewed YAML key spelling."""

import re

from postgres_scheduled_contract import EXPECTED_POSTGRES_SERVICE

ROOT_KEYS = frozenset({"name", "on", "permissions", "concurrency", "jobs"})
JOB_KEYS = frozenset({"if", "runs-on", "timeout-minutes", "services", "steps", "needs"})
STEP_KEYS = frozenset({"uses", "with", "run", "env", "working-directory", "if"})

# #9066: the reviewed workflow-level CGO_CFLAGS env stanza (see
# scripts/lib/cgo-cflags.sh and reducer_contention_events.CGO_ENV_STANZA, the same
# pattern). It cannot change job activation; any other env key still fails closed.
CGO_ENV_STANZA = re.compile(
    r'(?m)^(?:#.*\n)*env:\n  CGO_CFLAGS: "-O2 -g -std=gnu17"\n(?:\n|(?=\S))'
)


def without_cgo_env(workflow: str) -> str:
    """Return the workflow without the reviewed CGO_CFLAGS env stanza."""
    return CGO_ENV_STANZA.sub("", workflow, count=1)


JOB_ORDER = (
    "scheduled-setup",
    "scheduled-freshness-scale",
    "scheduled-freshness-a",
    "scheduled-freshness-b",
    "scheduled-ack",
    "scheduled-bulk",
    "scheduled-storage",
    "scheduled-aggregate",
)
SERVICE_JOBS = frozenset(JOB_ORDER[:7])
LEG_JOBS = frozenset(JOB_ORDER[1:7])
JOB_TIMEOUTS = {
    "scheduled-setup": "15",
    "scheduled-freshness-scale": "45",
    "scheduled-freshness-a": "25",
    "scheduled-freshness-b": "25",
    "scheduled-ack": "40",
    "scheduled-bulk": "60",
    "scheduled-storage": "40",
    "scheduled-aggregate": "15",
}
AGGREGATE_NEEDS = (
    "    needs: [scheduled-setup, scheduled-freshness-scale, "
    "scheduled-freshness-a, scheduled-freshness-b, scheduled-ack, "
    "scheduled-bulk, scheduled-storage]"
)
JOB_STEPS = {
    "scheduled-setup": (
        "Check out repository",
        "Set up Go",
        "Pre-warm Go modules",
        "Check exact enrollment and negative cases",
        "Record hosted proof manifest",
        "Upload hosted proof manifest",
    ),
    "scheduled-freshness-scale": (
        "Check out repository",
        "Set up Go",
        "Pre-warm Go modules",
        "Create fresh freshness scale database",
        "Run freshness scale proofs",
        "Upload freshness scale proof stream",
    ),
    "scheduled-freshness-a": (
        "Check out repository",
        "Set up Go",
        "Pre-warm Go modules",
        "Create fresh freshness chunk A database",
        "Run freshness chunk A proofs",
        "Upload freshness chunk A proof stream",
    ),
    "scheduled-freshness-b": (
        "Check out repository",
        "Set up Go",
        "Pre-warm Go modules",
        "Create fresh freshness chunk B database",
        "Run freshness chunk B proofs",
        "Upload freshness chunk B proof stream",
    ),
    "scheduled-ack": (
        "Check out repository",
        "Set up Go",
        "Pre-warm Go modules",
        "Create fresh ACK database",
        "Run ACK and completion proofs",
        "Upload ACK proof stream",
    ),
    "scheduled-bulk": (
        "Check out repository",
        "Set up Go",
        "Pre-warm Go modules",
        "Create fresh Crossplane database",
        "Run Crossplane proofs",
        "Create fresh inventory database",
        "Run infra inventory proofs",
        "Create fresh containerimage database",
        "Run containerimage proofs",
        "Run disposable golden corpus residual proof",
        "Run disposable query proofs",
        "Create fresh admin database",
        "Run admin proofs",
        "Create fresh completion database",
        "Run completion proofs",
        "Create fresh reachability database",
        "Run reachability proofs",
        "Run secret_lines proofs",
        "Upload bulk proof streams",
    ),
    "scheduled-storage": (
        "Check out repository",
        "Set up Go",
        "Pre-warm Go modules",
        "Create fresh claim database",
        "Run claim proofs",
        "Create fresh liveness database",
        "Run liveness proofs",
        "Create fresh fanout database",
        "Run fanout proofs",
        "Create fresh search database",
        "Run search proofs",
        "Create fresh readiness database",
        "Run readiness proofs",
        "Run disposable storage proofs",
        "Upload storage proof streams",
    ),
    "scheduled-aggregate": (
        "Check out repository",
        "Download proof streams",
        "Assemble hosted proof bundle",
        "Verify hosted proof bundle",
        "Retain hosted proof bundle",
    ),
}
ALWAYS_STEPS = frozenset({
    "Upload freshness scale proof stream",
    "Upload freshness chunk A proof stream",
    "Upload freshness chunk B proof stream",
    "Upload ACK proof stream",
    "Upload bulk proof streams",
    "Upload storage proof streams",
    "Retain hosted proof bundle",
})


def active_lines(text: str) -> list[str]:
    """Return non-empty, non-comment lines in order."""
    return [
        line for line in text.splitlines()
        if line.strip() and not line.lstrip().startswith("#")
    ]


def canonical_keys(text: str, indent: int, allowed: frozenset[str]) -> dict[str, str]:
    """Read literal keys at one indentation level and reject YAML aliases."""
    entries: dict[str, str] = {}
    for line in text.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        depth = len(line) - len(line.lstrip(" "))
        if depth != indent:
            continue
        match = re.fullmatch(rf" {{{indent}}}([a-z][a-z-]*):(?: .*)?", line)
        if match is None or match.group(1) not in allowed or match.group(1) in entries:
            raise ValueError(f"non-fail-closed conditional workflow key: {line}")
        entries[match.group(1)] = line
    return entries


def named_step(steps: list[str], name: str) -> tuple[int, str]:
    """Return the only active step with the exact reviewed name."""
    matches = [
        (index, step) for index, step in enumerate(steps)
        if step.startswith(f" {name}\n")
    ]
    if len(matches) != 1:
        raise ValueError(f"missing or duplicate hosted proof step: {name}")
    return matches[0]


def job_blocks(workflow: str) -> dict[str, str]:
    """Split the jobs mapping into one text block per reviewed job id."""
    jobs = workflow.split("\njobs:\n", 1)[1]
    blocks: dict[str, str] = {}
    current: str | None = None
    for line in jobs.splitlines(keepends=True):
        header = re.fullmatch(r"  ([a-z][a-z0-9-]*):\n", line)
        if header is not None:
            current = header.group(1)
            if current in blocks:
                raise ValueError(f"non-fail-closed duplicate workflow job: {current}")
            blocks[current] = ""
        elif current is not None:
            blocks[current] += line
    return blocks


def verify_canonical_workflow(workflow: str) -> None:
    """Reject alternate activation or failure-suppression keys in owned jobs."""
    workflow = without_cgo_env(workflow)
    root = canonical_keys(workflow, 0, ROOT_KEYS)
    if (set(root) != ROOT_KEYS or root["on"] != "on:"
            or root["jobs"] != "jobs:" or root["permissions"] != "permissions:"
            or root["concurrency"] != "concurrency:"
            or workflow.count("permissions:\n  contents: read\n") != 1):
        raise ValueError("non-fail-closed workflow root")
    permissions = re.search(r"(?ms)^permissions:\n(.*?)(?=^concurrency:)", workflow)
    concurrency = re.search(r"(?ms)^concurrency:\n(.*?)(?=^jobs:)", workflow)
    if (not permissions or canonical_keys(
            permissions.group(1), 2, frozenset({"contents"})
        ) != {"contents": "  contents: read"}):
        raise ValueError("non-fail-closed workflow permissions")
    concurrency_keys = frozenset({"group", "cancel-in-progress"})
    if (not concurrency or canonical_keys(
            concurrency.group(1), 2, concurrency_keys
        ) != {
            "group": "  group: postgres-scheduled-live-${{ github.event_name }}-${{ github.ref }}",
            "cancel-in-progress": "  cancel-in-progress: ${{ github.event_name == 'pull_request' }}",
        }):
        raise ValueError("non-fail-closed workflow PR concurrency")
    blocks = job_blocks(workflow)
    if list(blocks) != list(JOB_ORDER):
        raise ValueError("non-fail-closed workflow job header")
    for job, block in blocks.items():
        keys = canonical_keys(block, 4, JOB_KEYS)
        condition = ("    if: ${{ github.event_name != 'pull_request' || "
                     f"contains(fromJSON(inputs.plan_jobs || '[]'), '{job}') }}}}")
        if keys.get("if") != condition:
            raise ValueError(f"conditional scheduled live job: {job}")
        if keys.get("runs-on") != "    runs-on: ubuntu-24.04":
            raise ValueError(f"non-fail-closed workflow runner: {job}")
        if job in SERVICE_JOBS:
            if set(keys) != {"if", "runs-on", "timeout-minutes", "services", "steps"}:
                raise ValueError(f"non-fail-closed workflow job: {job}")
            if block.count("    services:\n") != 1:
                raise ValueError(f"non-fail-closed workflow services: {job}")
            svc = block.split("    services:\n", 1)[1].split("    steps:\n", 1)[0]
            if active_lines(svc) != EXPECTED_POSTGRES_SERVICE:
                raise ValueError(f"pinned PostgreSQL service block mismatch: {job}")
        elif job == "scheduled-aggregate":
            if set(keys) != {"if", "runs-on", "timeout-minutes", "steps", "needs"}:
                raise ValueError(f"non-fail-closed workflow job: {job}")
            if AGGREGATE_NEEDS not in block.splitlines():
                raise ValueError("non-fail-closed aggregate fan-in")
        else:
            raise ValueError(f"non-fail-closed workflow job: {job}")
        if block.count(f"    timeout-minutes: {JOB_TIMEOUTS[job]}\n") != 1:
            raise ValueError(f"non-fail-closed workflow timeout: {job}")
        step_list = block.split("    steps:\n", 1)[1]
        names = [
            line.removeprefix("      - name: ")
            for line in step_list.splitlines()
            if line.startswith("      ") and not line.startswith("       ")
            and line.strip() and not line.lstrip().startswith("#")
        ]
        if names != list(JOB_STEPS[job]):
            raise ValueError("non-fail-closed workflow step headers")
        for name, step in zip(names, re.split(r"(?m)^      - name: .+\n", step_list)[1:]):
            keys = canonical_keys(step, 8, STEP_KEYS)
            if not keys:
                raise ValueError(f"non-fail-closed workflow step: {name}")
            if "if" in keys:
                if name not in ALWAYS_STEPS or keys.get("if") != "        if: always()":
                    if name == "Retain hosted proof bundle":
                        raise ValueError("non-fail-closed artifact workflow step")
                    raise ValueError(f"non-fail-closed conditional workflow step: {name}")
            if name == "Check out repository":
                if active_lines(step) != [
                    "        uses: actions/checkout@v5",
                    "        with:",
                    "          persist-credentials: false",
                ]:
                    raise ValueError("non-fail-closed checkout step")
            if name == "Set up Go":
                if active_lines(step) != [
                    "        uses: actions/setup-go@v6",
                    "        with:",
                    "          go-version-file: go/go.mod",
                    "          cache-dependency-path: go/go.sum",
                    "          cache: false",
                ]:
                    raise ValueError("non-fail-closed Go setup action")
            if name == "Pre-warm Go modules":
                if active_lines(step) != ["        run: scripts/ci/go-mod-download-retry.sh"]:
                    raise ValueError("non-fail-closed Go module pre-warm step")


def verify_trigger(workflow: str, inventory: dict[str, dict[str, object]],
                   pr_paths: set[str], fragment: str) -> None:
    """Keep the daily jobs and frozen PR applicability enrolled together."""
    match = re.search(r"(?ms)^on:\n(.*?)(?=^permissions:)", workflow)
    if not match:
        raise ValueError("scheduled workflow event block missing")
    events = re.findall(r"(?m)^  ([a-z_]+):", match.group(1))
    if len(events) != 3 or set(events) != {"schedule", "workflow_dispatch", "workflow_call"}:
        raise ValueError("scheduled workflow PR events changed")
    if not re.search(r"(?m)^    - cron: '35 4 \* \* \*'$", match.group(1)):
        raise ValueError("scheduled workflow cadence missing")
    if not re.search(r"(?ms)^  workflow_call:\n    inputs:\n", match.group(1)):
        raise ValueError("scheduled workflow callable inputs missing")
    active = [line for line in fragment.splitlines()
              if line.strip() and not line.lstrip().startswith("#")]
    paths = sorted(pr_paths | set(inventory))
    expected = ["schema_version: 1", "workflows:",
                "- file: postgres-scheduled-live.yml",
                "  name: Scheduled PostgreSQL Live Proofs", "  synchronize: true",
                "  branches:", "  - main", "  paths:",
                *(f"  - {path}" for path in paths), "  jobs:",
                *(f"  - {job}" for job in sorted(JOB_ORDER)), "  job_names: {}"]
    if active != expected:
        raise ValueError("scheduled workflow PR paths or branches differ from exact enrollment")
    group = "  group: postgres-scheduled-live-${{ github.event_name }}-${{ github.ref }}"
    cancel = "  cancel-in-progress: ${{ github.event_name == 'pull_request' }}"
    if (len(re.findall("(?m)^" + re.escape(group) + "$", workflow)) != 1
            or len(re.findall("(?m)^" + re.escape(cancel) + "$", workflow)) != 1):
        raise ValueError("scheduled workflow PR concurrency is not separate")
    jobs = workflow.split("\njobs:\n", 1)
    if len(jobs) != 2 or re.findall(r"(?m)^  ([A-Za-z0-9_-]+):", jobs[1]) != list(JOB_ORDER):
        raise ValueError("scheduled workflow needs the same live job")
    if re.search(r"(?m)^\s*continue-on-error:", jobs[1]):
        raise ValueError("scheduled live job cannot use continue-on-error")


def verify_setup_steps(setup_steps: list[str]) -> None:
    """Require exact harness commands and hosted manifest recording."""
    bootstrap = [
        step for step in setup_steps
        if step.startswith(" Check exact enrollment and negative cases\n")
    ]
    if len(bootstrap) != 1:
        raise ValueError("scheduled runner harness step missing")
    expected_harness = [
        " Check exact enrollment and negative cases",
        "        run: |",
        "          python3 scripts/test-postgres-scheduled-runner.py",
        "          python3 scripts/test-postgres-scheduled-rerun.py",
        "          python3 scripts/test-postgres-scheduled-matrix.py",
        "          python3 scripts/test-postgres-scheduled-fragments.py",
        "          python3 scripts/test-postgres-scheduled-ledger-policy.py",
        "          python3 scripts/lib/postgres_scheduled_runner.py",
    ]
    bootstrap_lines = [
        line for line in bootstrap[0].splitlines()
        if line.strip() and not line.lstrip().startswith("#")
    ]
    if bootstrap_lines != expected_harness:
        raise ValueError("scheduled runner harness command missing or altered")
    if re.search(r"(?m)^\s*if:", bootstrap[0]):
        raise ValueError("conditional scheduled runner harness step")
    _, manifest_step = named_step(setup_steps, "Record hosted proof manifest")
    expected_manifest = [
        " Record hosted proof manifest",
        "        env:",
        "          POSTGRES_CONTAINER: ${{ job.services.postgres.id }}",
        "          PR_HEAD_SHA: ${{ github.event.pull_request.head.sha }}",
        "        run: |",
        "          set -euo pipefail",
        '          checkout_sha="$(git rev-parse HEAD)"',
        '          reviewed_head_sha="${PR_HEAD_SHA:-$checkout_sha}"',
        "          postgres_identity=\"$(docker exec \"$POSTGRES_CONTAINER\" "
        "psql -v ON_ERROR_STOP=1 -At -U eshu -d postgres -c 'SELECT version()')\"",
        "          python3 scripts/lib/postgres_scheduled_runner.py \\",
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
    ]
    manifest_actual = [
        line for line in manifest_step.splitlines()
        if line.strip() and not line.lstrip().startswith("#")
    ]
    if manifest_actual != expected_manifest:
        raise ValueError("hosted proof manifest command or identity missing")
    if re.search(r"(?m)^\s*if:", manifest_step):
        raise ValueError("conditional hosted proof manifest step")


FRESHNESS_SCALE_PREAMBLE = (
    "          ESHU_RETENTION_TIMING_MODE=build go test -race -p=1 -count=1 "
    "-timeout=10m ./internal/storage/postgres/freshness/links -run '^TestRetentionTimingP8$'",
    "          docker exec \"${{ job.services.postgres.id }}\" psql -v ON_ERROR_STOP=1 "
    "-U eshu -d postgres -c 'DROP DATABASE IF EXISTS eshu_freshness_scale WITH (FORCE)'",
    "          docker exec \"${{ job.services.postgres.id }}\" psql -v ON_ERROR_STOP=1 "
    "-U eshu -d postgres -c 'CREATE DATABASE eshu_freshness_scale'",
    "          docker exec -i \"${{ job.services.postgres.id }}\" psql -v ON_ERROR_STOP=1 "
    "-q -U eshu -d eshu_freshness_scale < internal/storage/postgres/migrations/001_ingestion_scopes.sql",
    "          docker exec -i \"${{ job.services.postgres.id }}\" psql -v ON_ERROR_STOP=1 "
    "-q -U eshu -d eshu_freshness_scale < internal/storage/postgres/migrations/002_scope_generations.sql",
    "          python3 -c 'import re; path=\"internal/storage/postgres/migrations/003_fact_records.sql\"; "
    "statements=[s.strip() for s in open(path).read().split(\";\")]; "
    "keep=[s for s in statements if s and not re.match(r\"(?is)^(--[^\\n]*\\n\\s*)*CREATE\\s+(UNIQUE\\s+)?INDEX\", s)]; "
    "print(\";\\n\".join(keep) + \";\")' | docker exec -i \"${{ job.services.postgres.id }}\" "
    "psql -v ON_ERROR_STOP=1 -q -U eshu -d eshu_freshness_scale",
    "          docker exec -i \"${{ job.services.postgres.id }}\" psql -v ON_ERROR_STOP=1 "
    "-q -U eshu -d eshu_freshness_scale < internal/storage/postgres/migrations/136_changed_since_link_ledger.sql",
    "          docker exec -i \"${{ job.services.postgres.id }}\" psql -v ON_ERROR_STOP=1 "
    "-q -U eshu -d eshu_freshness_scale < internal/storage/postgres/freshness/links/testdata/7127-link-writer-fixture.sql",
    "          docker exec \"${{ job.services.postgres.id }}\" psql -v ON_ERROR_STOP=1 "
    "-U eshu -d eshu_freshness_scale -c \"CREATE INDEX IF NOT EXISTS fact_records_scope_generation_idx "
    "ON fact_records (scope_id, generation_id, fact_kind, observed_at DESC)\"",
    "          docker exec \"${{ job.services.postgres.id }}\" psql -v ON_ERROR_STOP=1 "
    "-U eshu -d eshu_freshness_scale -c \"CREATE INDEX IF NOT EXISTS fact_records_scope_generation_keyset_idx "
    "ON fact_records (scope_id, generation_id, observed_at, fact_id)\"",
    "          docker exec \"${{ job.services.postgres.id }}\" psql -v ON_ERROR_STOP=1 "
    "-U eshu -d eshu_freshness_scale -c 'ANALYZE'",
)

GROUP_EXTRA_ENVS: dict[str, tuple[tuple[str, str], ...]] = {
    "admin": (("ESHU_ADMIN_REPLAY_FENCE_LIVE", '"1"'),),
    "crossplane": (("ESHU_CROSSPLANE_REDRIVE_PROOF_DISPOSABLE", "'1'"),),
    "claim": (
        ("ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DISPOSABLE", "'1'"),
        ("ESHU_POSTGRES_DSN", "postgres://eshu:change-me@localhost:15432/eshu_claim_scheduled?sslmode=disable"),
        ("ESHU_REDUCER_FAIRNESS_PROOF_DSN", "postgres://eshu:change-me@localhost:15432/eshu_claim_scheduled?sslmode=disable"),
    ),
    "fanout": (
        ("ESHU_REDUCER_FAIRNESS_PROOF_DSN", "postgres://eshu:change-me@localhost:15432/eshu_fanout_scheduled?sslmode=disable"),
    ),
    "search": (("ESHU_SEARCH_INDEX_PARTITION_LIVE", '"1"'),),
    "readiness": (
        ("ESHU_REDUCER_FAIRNESS_PROOF_DISPOSABLE", "'1'"),
        ("ESHU_POSTGRES_DSN", "postgres://eshu:change-me@localhost:15432/eshu_readiness_scheduled?sslmode=disable"),
    ),
    "ack_family": (
        ("ESHU_POSTGRES_DSN", "postgres://eshu:change-me@localhost:15432/eshu_ack_scheduled?sslmode=disable"),
    ),
    "freshness_links_a": (("ESHU_7127_W11_COST", '"1"'),),
    "freshness_links_scale": (
        ("ESHU_7127_W11_COST", '"1"'),
        ("ESHU_RETENTION_TIMING_DSN", "postgres://eshu:change-me@localhost:15432/eshu_freshness_scheduled?sslmode=disable"),
        ("ESHU_RETENTION_TIMING_LABEL", '"freshness-links-scheduled"'),
        ("ESHU_RETENTION_TIMING_FIXTURES", '"link"'),
        ("ESHU_RETENTION_TIMING_FIXTURE", '"link"'),
        ("ESHU_RETENTION_TIMING_MODE", '"run"'),
        ("ESHU_CHANGED_SINCE_LINK_SCALE_DSN", "postgres://eshu:change-me@localhost:15432/eshu_freshness_scale?sslmode=disable"),
        ("ESHU_CHANGED_SINCE_LINK_SCALE_BASE_REF", '"HEAD"'),
        ("ESHU_CHANGED_SINCE_LINK_SCALE_ROUNDS", '"1"'),
        ("ESHU_CHANGED_SINCE_LINK_SCALE_VALID_ROUNDS", '"1"'),
        ("ESHU_CHANGED_SINCE_LINK_SCALE_DEADLINE", '"30m"'),
        ("ESHU_CHANGED_SINCE_LINK_SCALE_PROOF", '"p3"'),
    ),
}


def verify_proof_step_exact(
    group: str, step: str, package_path: str, timeout: str, arguments: str,
    dsns: dict[str, str], databases: dict[str, str], admin_envs: dict[str, tuple[str, ...]],
) -> None:
    """Require exact working-directory, env map, and run lines for a proof step."""
    lines = active_lines(step)
    if len(lines) < 4 or lines[1] != "        working-directory: go" or lines[2] != "        env:":
        raise ValueError(f"non-fail-closed proof step header for {group}")
    try:
        run_index = lines.index("        run: |")
    except ValueError as err:
        raise ValueError(f"non-fail-closed proof step run block for {group}") from err
    expected_env: set[str] = set()
    if group in dsns:
        dsn = f"postgres://eshu:change-me@localhost:15432/{databases[group]}?sslmode=disable"
        expected_env.add(f"          {dsns[group]}: {dsn}")
        for key, value in GROUP_EXTRA_ENVS.get(group, ()):
            expected_env.add(f"          {key}: {value}")
    else:
        for prefix in admin_envs[group]:
            expected_env.add(f"          {prefix}_DSN: postgres://eshu:change-me@localhost:15432/postgres?sslmode=disable")
            expected_env.add(f'          {prefix}_DISPOSABLE: "1"')
    actual_env = lines[3:run_index]
    if len(actual_env) != len(set(actual_env)) or set(actual_env) != expected_env:
        raise ValueError(f"non-fail-closed proof step env for {group}")
    expected_run = [
        "          set -euo pipefail",
        '          test -z "${GOFLAGS:-}"',
        '          test -z "${GOWORK:-}"',
        '          effective_config="$(go env GOFLAGS GOWORK)"',
        '          test -z "$effective_config"',
        '          proof_status="$(git -C .. status --porcelain --untracked-files=all)"',
        '          test -z "$proof_status"',
        *(FRESHNESS_SCALE_PREAMBLE if group == "freshness_links_scale" else ()),
        f'          selector="$(python3 ../scripts/lib/postgres_scheduled_runner.py --selector --group {group})"',
        f"          go test -json -race -p=1 -count=1 -timeout={timeout} {package_path} "
        f'-run "${{selector}}"{arguments} | tee "${{RUNNER_TEMP}}/{group}.jsonl"',
        f'          python3 ../scripts/lib/postgres_scheduled_runner.py --events "${{RUNNER_TEMP}}/{group}.jsonl" --group {group}',
        '          effective_config="$(go env GOFLAGS GOWORK)"',
        '          test -z "$effective_config"',
        '          proof_status="$(git -C .. status --porcelain --untracked-files=all)"',
        '          test -z "$proof_status"',
    ]
    if lines[run_index + 1:] != expected_run:
        raise ValueError(f"non-fail-closed proof step command body for {group}")
