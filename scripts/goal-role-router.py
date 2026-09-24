#!/usr/bin/env python3
"""Add manifest-backed role routing context to explicit Eshu goal prompts."""

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = json.loads((ROOT / ".agents/roles.json").read_text())
SKILL_NAMES = sorted(
    (path.parent.name for path in (ROOT / ".agents/skills").glob("*/SKILL.md")),
    key=len,
    reverse=True,
)
SKILL_PATTERN = re.compile(
    r"(?<![\w-])(?:" + "|".join(map(re.escape, SKILL_NAMES)) + r")(?![\w-])"
)


def goal_text(prompt: str) -> str:
    stripped = prompt.strip()
    if stripped.startswith("/goal "):
        body = stripped[6:].strip()
    elif stripped.startswith("GOAL:"):
        body = stripped[5:].strip()
    else:
        return ""
    if not body or body.split()[0] in {"done", "clear", "consent", "revoke-consent"}:
        return ""
    # Claude users commonly pass a prepared goal file. Read only that explicit
    # file, capped so an accidental large path cannot flood hook context.
    if len(body) < 1024 and not any(char.isspace() for char in body):
        candidate = Path(body)
        if candidate.is_file() and candidate.stat().st_size <= 65536:
            return candidate.read_text(errors="replace")
    return body


def route(prompt: str, harness: str) -> str:
    body = goal_text(prompt)
    if not body:
        return ""
    skills = set(SKILL_PATTERN.findall(body))
    if not skills:
        return ""
    lower = SKILL_PATTERN.sub(" ", body.lower())
    roles = []
    if re.search(r"\b(scan|inventory|gather evidence)\b", lower):
        roles.append("scan-eshu")
    deep = bool(re.search(r"\b(intermittent|cross.system|difficult|deep diagnosis|deep performance)\b", lower))
    if "eshu-diagnostic-rigor" in skills or re.search(r"\b(debug|diagnos|root cause|reproduc)", lower):
        roles.append("debug-eshu-deep" if deep else "debug-eshu")
    if "eshu-performance-rigor" in skills or re.search(r"\b(benchmark|profil|bottleneck|latency)", lower):
        roles.append("perf-eshu-deep" if deep else "perf-eshu")
    if re.search(r"\b(implement|fix|patch|code|build)\b", lower):
        roles.append("develop-eshu")
    if "eshu-code-review" in skills or re.search(r"\b(review|pr.readiness)\b", lower):
        roles.append("review-eshu")
    if not roles:
        roles.append("scan-eshu")
    # A named coordinator skill does not become a leaf-agent job.
    lines = ["Eshu goal role hints (phase candidates, not an execution order):"]
    if "eshu-issue-driver" in skills:
        lines.append("- eshu-issue-driver stays with the main coordinator for issue/PR ownership.")
    if "concurrency-deadlock-rigor" in skills:
        lines.append("- concurrency-deadlock-rigor is a method for the relevant worker, not a separate role.")
    bindings = MANIFEST["models"].get(harness, {})
    for name in dict.fromkeys(roles):
        role = MANIFEST["roles"][name]
        if "base" in role:
            role = {**MANIFEST["roles"][role["base"]], **role}
        tier = role["tier"]
        binding = bindings.get(tier)
        model = f"; {binding['model']} effort={binding['effort']}" if binding else ""
        lines.append(f"- {name}: {role['description']} ({tier}; {role['access']}{model})")
    if harness == "claude":
        lines.append("Delegate a bounded phase to its native .claude/agents role when useful.")
    elif harness in {"codex", "muse"}:
        lines.append(f"When the native child tool cannot select this role and binding, the coordinator can run scripts/agent-roles.py {harness}-exec ROLE TASK.")
    lines.append("Preserve the goal's explicit phases and model choices. Do not assign the whole goal from this hint; the main session model is unchanged.")
    return "\n".join(lines)


def main() -> None:
    try:
        payload = json.load(sys.stdin)
        harness = sys.argv[1] if len(sys.argv) > 1 else ""
        context = route(str(payload.get("prompt", "")), harness)
        if context:
            print(json.dumps({"hookSpecificOutput": {"hookEventName": "UserPromptSubmit", "additionalContext": context}}))
    except (OSError, ValueError, TypeError, KeyError):
        # Prompt hooks must not prevent the user's message from being sent.
        return


if __name__ == "__main__":
    main()
