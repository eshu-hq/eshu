"""Check active replay workflow and registry routes."""

from __future__ import annotations

from pathlib import Path
import re


ROUTED_PATHS = (
    "scripts/lib/replay_tier_enrollment.py",
    "scripts/lib/replay_tier_routes.py",
    "scripts/lib/replay_tier_inventory.json",
    "scripts/test-replay-tier-enrollment.py",
    "scripts/test-replay-tier-ledger-policy.py",
    "scripts/lib/verify-live-tests-ledger.py",
    "specs/live-tests.v1.yaml",
    "specs/live-tests.d/**",
    "specs/ci-gates.d/gates-15.yaml",
)


def verify_workflow_liveness(workflow: str) -> None:
    """Require the existing CI job to execute every reviewed proof step."""
    if (not all(re.search(pattern, workflow, re.M) for pattern in (
        r"^  merge_group:\n    types: \[checks_requested\]$",
        r"^  push:\n    branches: \[ main \]$",
        r"^  pull_request:\n    branches: \[ main \]$",
    )) or any(len(re.findall(pattern, workflow, re.M)) != 1 for pattern in (
        r"^on:.*$", r"^jobs:.*$", r"^  merge_group:.*$",
        r"^  push:.*$", r"^  pull_request:.*$",
    ))):
        raise ValueError("workflow events no longer select the replay gate")
    sections = workflow.split("\njobs:\n", 1)
    if len(sections) != 2:
        raise ValueError("workflow jobs block is missing")
    job_rows = re.findall(
        r"(?ms)^  ([a-z][a-z0-9-]*):\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)",
        sections[1],
    )
    if [name for name, _ in job_rows] != ["queue-selection", "replay-tier"]:
        raise ValueError("workflow queue or replay job is missing or replaced")
    queue, replay = (body for _, body in job_rows)
    queue_fields = re.findall(r"(?m)^    ([a-z][a-z0-9-]*):(?: ([^\n]+))?$", queue)
    replay_fields = re.findall(r"(?m)^    ([a-z][a-z0-9-]*):(?: ([^\n]+))?$", replay)
    if (queue_fields != [("uses", "./.github/workflows/queue-selection.yml"),
                         ("with", "")]
            or re.findall(r"(?m)^      ([a-z][a-z0-9-]*):(?: ([^\n]+))?$",
                          queue) != [("workflow", "verify-replay-tier.yml")]
            or replay_fields != [
                ("needs", "queue-selection"),
                ("name", "Offline replay tier vs real NornicDB"),
                ("runs-on", "ubuntu-latest"),
                ("timeout-minutes", "15"),
                ("steps", ""),
            ]):
        raise ValueError("workflow replay job or queue selection is disabled")
    steps_text = replay.split("    steps:\n", 1)[1]
    steps = re.findall(r"(?ms)^      - name: ([^\n]+)\n(.*?)(?=^      - |\Z)",
                       steps_text)
    if len(steps) != 7 or len(re.findall(r"(?m)^      - ", steps_text)) != 7:
        raise ValueError("workflow proof steps are missing or replaced")
    selection = ("${{ github.event_name != 'merge_group' || "
                 "(contains(fromJSON(needs.queue-selection.outputs.jobs || '[]'), "
                 "'Offline replay tier vs real NornicDB')) }}")
    expected = (
        ("Require queue selection to succeed",
         "${{ always() && github.event_name == 'merge_group' }}",
         "run", 'test "${{ needs.queue-selection.result }}" = success'),
        ("Check out code", selection, "uses", "actions/checkout@v5"),
        ("Set up Go", selection, "uses", "actions/setup-go@v6"),
        ("Install contract dependencies", selection, "run",
         "scripts/ci/install-apt-packages.sh ripgrep"),
        ("Pre-warm Go modules", selection, "run",
         "scripts/ci/go-mod-download-retry.sh"),
        ("Verify replay tier contract", selection, "run",
         "bash scripts/test-verify-replay-tier.sh"),
        ("Run offline replay tier against real NornicDB", selection, "run",
         "bash scripts/verify-replay-tier.sh"),
    )
    for (name, body), (want_name, want_if, command_key, command) in zip(steps, expected):
        fields = re.findall(r"(?m)^        ([a-z][a-z0-9-]*):(?: ([^\n]+))?$",
                            body)
        want_fields = [("if", want_if), (command_key, command)]
        if name == "Set up Go":
            want_fields.append(("with", ""))
            if re.findall(
                r"(?m)^          ([a-z][a-z0-9-]*):(?: ([^\n]+))?$", body
            ) != [("go-version-file", "go/go.mod"),
                  ("cache-dependency-path", "go/go.sum")]:
                raise ValueError("workflow Go setup no longer uses reviewed module")
        if name != want_name or fields != want_fields:
            raise ValueError(f"workflow proof step disabled or altered: {want_name}")


def _active_lines(source: str, label: str) -> list[tuple[int, str]]:
    """Keep active YAML lines and reject ambiguous tab indentation."""
    active = []
    for line in source.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        prefix = line[:len(line) - len(line.lstrip())]
        if "\t" in prefix:
            raise ValueError(f"{label} uses unsupported tab indentation")
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
    gates = _only_block(lines, "gates", 0, "registry")
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
    return _paths(triggers, 6, '"', "registry replay-tier.triggers")


def _prepr_selector(prepr: str) -> re.Pattern[str]:
    """Extract the one active replay command's literal path regex."""
    lines = prepr.splitlines()
    starts = [index for index, line in enumerate(lines)
              if re.fullmatch(r"[ \t]*run_or_defer replay-tier \\", line)]
    if len(starts) != 1 or starts[0] + 3 >= len(lines):
        raise ValueError("pre-pr needs one active replay-tier command")
    index = starts[0]
    selector = re.fullmatch(r"[ \t]*'([^']+)' \\", lines[index + 1])
    if (selector is None
            or re.fullmatch(r"[ \t]*'docker info' \\", lines[index + 2]) is None
            or re.fullmatch(r'[ \t]*bash "\$\{repo_root\}/scripts/verify-replay-tier\.sh" \|\| rc=1',
                            lines[index + 3]) is None):
        raise ValueError("pre-pr replay-tier command has unsupported arguments")
    try:
        return re.compile(selector.group(1))
    except re.error as error:
        raise ValueError("pre-pr replay-tier selector is invalid") from error


def verify_routes(root: Path) -> None:
    """Require metadata-only PRs to select the existing blocking replay job."""
    workflow = (root / ".github/workflows/verify-replay-tier.yml").read_text()
    verify_workflow_liveness(workflow)
    registry = (root / "specs/ci-gates.d/gates-15.yaml").read_text()
    prepr = (root / "scripts/dev/pre-pr.sh").read_text()
    workflow_triggers = workflow_paths(workflow)
    registry_triggers = replay_gate_triggers(registry)
    selector = _prepr_selector(prepr)
    for path in ROUTED_PATHS:
        if path not in workflow_triggers:
            raise ValueError(f"workflow lacks replay metadata trigger: {path}")
        if path.startswith("scripts/lib/replay_tier_"):
            registry_path = "scripts/lib/replay_tier_*"
        elif path == "scripts/lib/verify-live-tests-ledger.py":
            registry_path = "scripts/lib/verify-live-tests-ledger.py"
        elif path.startswith("scripts/test-replay-tier-"):
            registry_path = "scripts/test-replay-tier-*.py"
        elif path.startswith("specs/live-tests"):
            registry_path = path
        else:
            registry_path = path
        if registry_path not in registry_triggers:
            raise ValueError(f"registry lacks replay metadata trigger: {path}")
        sample = ("specs/live-tests.d/live-tests-03.yaml"
                  if path == "specs/live-tests.d/**" else path)
        if selector.match(sample) is None:
            raise ValueError(f"pre-pr lacks replay metadata trigger: {path}")
