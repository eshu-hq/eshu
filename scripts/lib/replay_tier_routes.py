"""Check active replay workflow and registry routes."""

from __future__ import annotations

from fnmatch import fnmatchcase
from pathlib import Path
import re


ROUTED_PATHS = (
    "scripts/lib/replay_tier_enrollment.py",
    "scripts/lib/replay_tier_routes.py",
    "scripts/lib/replay_tier_inventory.json",
    "scripts/test-replay-tier-enrollment.py",
    "scripts/test-replay-tier-source-integrity.py",
    "scripts/test-replay-tier-ledger-policy.py",
    "scripts/lib/verify-live-tests-ledger.py",
    "specs/live-tests.v1.yaml",
    "specs/live-tests.d/**",
    "specs/ci-gates.d/gates-15.yaml",
    "specs/ci-gates.d/gates-09.yaml",
    ".github/workflows/live-backend-tests.yml",
)

INDEPENDENT_SUBJECTS = (
    "scripts/verify-replay-tier.sh",
    "scripts/lib/replay_tier_enrollment.py",
    "scripts/lib/replay_tier_routes.py",
    "scripts/lib/replay_tier_inventory.json",
    "scripts/test-replay-tier-source-integrity.py",
)


# The reviewed workflow and gate use a literal YAML subset. Compare every active
# control line, then validate the one variable paths/triggers sequence separately.
WORKFLOW_CONTROL_LINES = """
name: Replay Tier (offline NornicDB)
on:
  merge_group:
    types: [checks_requested]
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]
    paths:
concurrency:
  group: ${{ github.workflow }}-${{ github.event_name == 'push' && github.ref == 'refs/heads/main' && github.sha || github.ref }}
  cancel-in-progress: ${{ github.event_name == 'pull_request' }}
permissions:
  contents: read
jobs:
  queue-selection:
    uses: ./.github/workflows/queue-selection.yml
    with:
      workflow: verify-replay-tier.yml
  replay-tier:
    needs: queue-selection
    name: Offline replay tier vs real NornicDB
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - name: Require queue selection to succeed
        if: ${{ always() && github.event_name == 'merge_group' }}
        run: test "${{ needs.queue-selection.result }}" = success
      - name: Check out code
        if: ${{ github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'Offline replay tier vs real NornicDB')) }}
        uses: actions/checkout@v5
      - name: Set up Go
        if: ${{ github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'Offline replay tier vs real NornicDB')) }}
        uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
      - name: Install contract dependencies
        if: ${{ github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'Offline replay tier vs real NornicDB')) }}
        run: scripts/ci/install-apt-packages.sh ripgrep
      - name: Pre-warm Go modules
        if: ${{ github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'Offline replay tier vs real NornicDB')) }}
        run: scripts/ci/go-mod-download-retry.sh
      - name: Verify replay tier contract
        if: ${{ github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'Offline replay tier vs real NornicDB')) }}
        run: bash scripts/test-verify-replay-tier.sh
      - name: Run offline replay tier against real NornicDB
        if: ${{ github.event_name != 'merge_group' || (contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), 'Offline replay tier vs real NornicDB')) }}
        run: bash scripts/verify-replay-tier.sh
""".strip("\n").splitlines()

REGISTRY_GATE_CONTROL_LINES = """
  - id: replay-tier
    name: Replay tier (offline cassette replay)
    category: exactness
    layer: replay
    purpose: "Replays recorded ingest/projector cassettes offline and checks the results match the real NornicDB backend."
    tier: ci-heavy
    blocking: true
    triggers:
    local:
      command: "bash scripts/verify-replay-tier.sh"
      test_command: "bash scripts/test-verify-replay-tier.sh"
    ci:
      workflow: verify-replay-tier.yml
      job: "Offline replay tier vs real NornicDB"
    requirements:
      - go
      - docker
    ci_only_reason: ""
""".strip("\n").splitlines()

# A fixed reviewed selector keeps the live-tests.d/ branch open for every
# current and future fragment, not merely the fragment used in one fixture.
PREPR_SELECTOR = (
    r"^(go/cmd/(ingester|projector)/"
    r"|go/internal/(query|replay|reducer|storage/cypher|storage/nornicdb"
    r"|projector|graph|runtime)/"
    r"|testdata/cassettes/(replayoffline|replaydelta)/"
    r"|scripts/(verify-replay-tier|test-verify-replay-tier)\.sh"
    r"|scripts/dev/pre-pr\.sh|scripts/ci/install-apt-packages\.sh"
    r"|\.github/workflows/(verify-replay-tier|live-backend-tests)\.yml"
    r"|scripts/lib/(replay_tier_|verify-live-tests-ledger\.py)"
    r"|scripts/test-replay-tier-.*\.py"
    r"|specs/live-tests(\.v1\.yaml|\.d/)"
    r"|specs/ci-gates\.d/gates-(09|15)\.yaml)"
)


def _control_lines_without_sequence(lines: list[tuple[int, str]],
                                    key: str, indent: int,
                                    label: str) -> list[str]:
    """Keep every active control line outside one validated literal sequence."""
    indexes = [index for index, (depth, content) in enumerate(lines)
               if depth == indent and content == key + ":"]
    if len(indexes) != 1:
        raise ValueError(f"{label} needs one literal {key} sequence")
    start = indexes[0] + 1
    end = next((index for index in range(start, len(lines))
                if lines[index][0] <= indent), len(lines))
    return [" " * depth + content for index, (depth, content) in enumerate(lines)
            if not start <= index < end]


def verify_workflow_liveness(workflow: str) -> None:
    """Require the existing CI job to execute every reviewed proof step."""
    workflow_paths(workflow)
    controls = _control_lines_without_sequence(
        _active_lines(workflow, "workflow"), "paths", 4, "workflow"
    )
    if controls != WORKFLOW_CONTROL_LINES:
        raise ValueError("workflow has unsupported active YAML controls")


def _active_lines(source: str, label: str) -> list[tuple[int, str]]:
    """Keep active YAML lines and reject ambiguous tab indentation."""
    active = []
    for line in source.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        prefix = line[:len(line) - len(line.lstrip())]
        if any(character != " " for character in prefix):
            raise ValueError(f"{label} uses unsupported indentation")
        active.append((len(prefix), line.strip()))
    return active


def _reject_quoted_workflow_keys(lines: list[tuple[int, str]]) -> None:
    """Reject quoted mapping keys outside the reviewed workflow subset."""
    for _, content in lines:
        if re.match(r"^(['\"])[^'\"]+\1\s*:", content):
            raise ValueError("workflow uses unsupported quoted mapping key")


def _only_block(lines: list[tuple[int, str]], key: str, indent: int,
                label: str) -> list[tuple[int, str]]:
    """Return the one block for an active mapping key at its exact depth."""
    matches = [index for index, (depth, content) in enumerate(lines)
               if depth == indent
               and content.split(":", 1)[0].strip("\"'") == key]
    if len(matches) != 1 or lines[matches[0]][1] != key + ":":
        raise ValueError(f"{label} needs one active {key} mapping")
    start = matches[0] + 1
    end = next((index for index in range(start, len(lines))
                if lines[index][0] <= indent), len(lines))
    return lines[start:end]


def _paths(lines: list[tuple[int, str]], indent: int, quote: str,
           label: str) -> set[str]:
    """Read only literal paths in the selected YAML sequence."""
    paths = set()
    pattern = re.compile(r"- " + re.escape(quote) + r"([^" + re.escape(quote)
                         + r"]+)" + re.escape(quote))
    for depth, content in lines:
        match = pattern.fullmatch(content)
        if depth != indent or match is None or match.group(1) in paths:
            raise ValueError(f"{label} has an ambiguous path sequence")
        paths.add(match.group(1))
    if not paths:
        raise ValueError(f"{label} has no active paths")
    return paths


def workflow_paths(workflow: str) -> set[str]:
    """Read effective on.pull_request.paths in the supported workflow shape."""
    lines = _active_lines(workflow, "workflow")
    _reject_quoted_workflow_keys(lines)
    events = _only_block(lines, "on", 0, "workflow")
    pull_request = _only_block(events, "pull_request", 2, "workflow")
    if any(content.startswith("<<:") for _, content in pull_request):
        raise ValueError("workflow pull_request uses unsupported YAML merge")
    paths = _only_block(pull_request, "paths", 4, "workflow")
    return _paths(paths, 6, "'", "workflow pull_request.paths")


def replay_gate_triggers(registry: str) -> set[str]:
    """Read effective replay-tier triggers in the supported registry shape."""
    lines = _active_lines(registry, "registry")
    if [content for depth, content in lines if depth == 0] != [
        "version: v1", "gates:"
    ]:
        raise ValueError("registry has unsupported root controls")
    gates = _only_block(lines, "gates", 0, "registry")
    if any(depth == 2 and re.fullmatch(
        r"- id: [a-z][a-z0-9-]*(?: #.*)?", content
    ) is None for depth, content in gates):
        raise ValueError("registry has unsupported gate entries")
    matches = [index for index, (depth, content) in enumerate(gates)
               if depth == 2 and content.startswith("- id:")
               and content.partition(":")[2].split(" #", 1)[0]
               .strip().strip("\"'") == "replay-tier"]
    if len(matches) != 1:
        raise ValueError("registry needs one replay-tier gate")
    start = matches[0] + 1
    end = next((index for index in range(start, len(gates))
                if gates[index][0] <= 2), len(gates))
    gate = gates[start:end]
    if any(content.startswith("<<:") for _, content in gate):
        raise ValueError("registry replay-tier uses unsupported YAML merge")
    triggers = _only_block(gate, "triggers", 4, "registry")
    paths = _paths(triggers, 6, '"', "registry replay-tier.triggers")
    controls = _control_lines_without_sequence(
        [gates[matches[0]], *gate], "triggers", 4, "registry"
    )
    if controls != REGISTRY_GATE_CONTROL_LINES:
        raise ValueError("registry replay-tier has unsupported active YAML controls")
    return paths


def _prepr_selector(prepr: str) -> re.Pattern[str]:
    """Extract the one active replay command's literal path regex."""
    lines = prepr.splitlines()
    starts = [index for index, line in enumerate(lines)
              if re.fullmatch(r"[ \t]*run_or_defer replay-tier \\", line)]
    if len(starts) != 1 or starts[0] + 3 >= len(lines):
        raise ValueError("pre-pr needs one active replay-tier command")
    index = starts[0]
    functions = [position for position, line in enumerate(lines)
                 if line == "step_live() {"]
    calls = [position for position, line in enumerate(lines)
             if line == 'run_step "path-triggered live lane" step_live']
    if len(functions) != 1 or len(calls) != 1:
        raise ValueError("pre-pr no longer invokes its live lane")
    end = next((position for position in range(functions[0] + 1, len(lines))
                if lines[position] == "}"), -1)
    if not functions[0] < index < end < calls[0]:
        raise ValueError("pre-pr replay command is outside the live lane")
    selector = re.fullmatch(r"[ \t]*'([^']+)' \\", lines[index + 1])
    if (selector is None
            or re.fullmatch(r"[ \t]*'docker info' \\", lines[index + 2]) is None
            or re.fullmatch(r'[ \t]*bash "\$\{repo_root\}/scripts/verify-replay-tier\.sh" \|\| rc=1',
                            lines[index + 3]) is None):
        raise ValueError("pre-pr replay-tier command has unsupported arguments")
    if selector.group(1) != PREPR_SELECTOR:
        raise ValueError("pre-pr replay-tier selector differs from reviewed coverage")
    try:
        return re.compile(selector.group(1))
    except re.error as error:
        raise ValueError("pre-pr replay-tier selector is invalid") from error


def verify_independent_static_mirror(root: Path) -> None:
    """Require replay source edits to select an active independent ledger read."""
    workflow = (root / ".github/workflows/live-backend-tests.yml").read_text()
    lines = _active_lines(workflow, "independent workflow")
    _reject_quoted_workflow_keys(lines)
    events = _only_block(lines, "on", 0, "independent workflow")
    pull_request = _only_block(events, "pull_request", 2, "independent workflow")
    path_lines = _only_block(pull_request, "paths", 4, "independent workflow")
    paths = set()
    for depth, content in path_lines:
        match = re.fullmatch(r"- '([^']+)'", content)
        if depth != 6 or match is None:
            raise ValueError("independent workflow has unsupported PR paths")
        paths.add(match.group(1))
    required = set(INDEPENDENT_SUBJECTS) | {
        ".github/workflows/live-backend-tests.yml", "specs/ci-gates.d/gates-09.yaml"
    }
    if not required <= paths:
        raise ValueError("independent workflow does not select replay proof sources")

    jobs = _only_block(lines, "jobs", 0, "independent workflow")
    job = _only_block(jobs, "static-mirror", 2, "independent workflow")
    job_keys = [content.split(":", 1)[0] for depth, content in job if depth == 4]
    if job_keys != ["needs", "name", "runs-on", "timeout-minutes", "steps"]:
        raise ValueError("independent workflow static mirror is disabled or changed")
    if (4, "needs: queue-selection") not in job or (
            4, "name: static-mirror (ledger and runner probes)") not in job:
        raise ValueError("independent workflow static mirror is not selected")
    steps = _only_block(job, "steps", 4, "independent workflow")
    starts = [index for index, line in enumerate(steps)
              if line == (6, "- name: Validate the live-test ledger")]
    if len(starts) != 1:
        raise ValueError("independent workflow ledger step is missing")
    end = next((index for index in range(starts[0] + 1, len(steps))
                if steps[index][0] <= 6), len(steps))
    selected = ("if: ${{ github.event_name != 'merge_group' || "
                "(contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), "
                "'static-mirror (ledger and runner probes)')) }}")
    if steps[starts[0]:end] != [
        (6, "- name: Validate the live-test ledger"),
        (8, selected),
        (8, "run: bash scripts/verify-live-tests-ledger.sh"),
    ]:
        raise ValueError("independent workflow ledger step is inactive")

    registry = (root / "specs/ci-gates.d/gates-09.yaml").read_text()
    registry_lines = _active_lines(registry, "independent registry")
    if [content for depth, content in registry_lines if depth == 0] != [
        "version: v1", "gates:"
    ]:
        raise ValueError("independent registry has unsupported root controls")
    gates = _only_block(registry_lines, "gates", 0, "independent registry")
    starts = [index for index, line in enumerate(gates)
              if line == (2, "- id: live-backend-static-mirror")]
    if len(starts) != 1:
        raise ValueError("independent registry static mirror is missing")
    end = next((index for index in range(starts[0] + 1, len(gates))
                if gates[index][0] <= 2), len(gates))
    gate = gates[starts[0]:end]
    keys = [content.split(":", 1)[0] for depth, content in gate if depth == 4]
    if keys != ["name", "category", "layer", "purpose", "tier", "blocking",
                "triggers", "local", "ci", "requirements", "ci_only_reason"]:
        raise ValueError("independent registry static mirror controls changed")
    if (4, "tier: pre-pr") not in gate or (4, "blocking: true") not in gate:
        raise ValueError("independent registry static mirror is not blocking")
    triggers = _paths(_only_block(gate, "triggers", 4, "independent registry"),
                      6, '"', "independent registry triggers")
    if not required <= triggers:
        raise ValueError("independent registry does not select replay proof sources")
    local = _only_block(gate, "local", 4, "independent registry")
    if [content for depth, content in local if depth == 6 and content.startswith("command:")] != [
        'command: "bash scripts/verify-live-tests-ledger.sh"'
    ]:
        raise ValueError("independent registry does not run the ledger")
    ci = _only_block(gate, "ci", 4, "independent registry")
    if ci != [(6, "workflow: live-backend-tests.yml"),
              (6, 'job: "static-mirror (ledger and runner probes)"')]:
        raise ValueError("independent registry does not own the static mirror job")


def verify_routes(root: Path) -> None:
    """Require metadata-only PRs to select the existing blocking replay job."""
    verify_independent_static_mirror(root)
    workflow = (root / ".github/workflows/verify-replay-tier.yml").read_text()
    verify_workflow_liveness(workflow)
    registry = (root / "specs/ci-gates.d/gates-15.yaml").read_text()
    prepr = (root / "scripts/dev/pre-pr.sh").read_text()
    workflow_triggers = workflow_paths(workflow)
    registry_triggers = replay_gate_triggers(registry)
    selector = _prepr_selector(prepr)
    if workflow_triggers != registry_triggers:
        raise ValueError("replay registry and workflow trigger sets differ")
    for path in ROUTED_PATHS:
        if not any(fnmatchcase(path, trigger) for trigger in workflow_triggers):
            raise ValueError(f"workflow lacks replay metadata trigger: {path}")
        if path == "specs/live-tests.d/**":
            fragments = list((root / "specs/live-tests.d").glob("**/*.yaml"))
            if not fragments:
                raise ValueError("pre-pr has no live-test fragments to check")
            samples = {item.relative_to(root).as_posix() for item in fragments}
            samples.update(("specs/live-tests.d/live-tests-99.yaml",
                            "specs/live-tests.d/future/live-tests-99.yaml"))
        else:
            samples = {path}
        for sample in samples:
            if selector.match(sample) is None:
                raise ValueError(f"pre-pr lacks replay metadata trigger: {sample}")
