#!/usr/bin/env python3
"""Add manifest-backed role routing context to explicit Eshu goal prompts."""

import json
import re
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SKILL_NAMES = sorted(
    (path.parent.name for path in (ROOT / ".agents/skills").glob("*/SKILL.md")),
    key=len,
    reverse=True,
)
SKILL_PATTERN = re.compile(
    r"(?<![\w-])(?:" + "|".join(map(re.escape, SKILL_NAMES)) + r")(?![\w-])"
)


def prepared_goal(body: str, cwd: str) -> str:
    if len(body) >= 1024 or "\n" in body:
        return ""
    candidate = Path(body)
    if candidate.name not in {"goal.txt", "goal.md"} or not cwd:
        return ""
    workspace = Path(cwd).resolve()
    if not candidate.is_absolute():
        candidate = workspace / candidate
    if candidate.is_symlink() or not candidate.is_file() or candidate.stat().st_size > 65536:
        return ""
    resolved = candidate.resolve()
    in_workspace = resolved.is_relative_to(workspace)
    slug = str(workspace).replace("/", "-")
    temp_roots = {Path(tempfile.gettempdir()).resolve(), Path("/private/tmp").resolve()}
    in_claude_scratchpad = (
        len(resolved.parents) >= 5
        and resolved.parents[0].name == "scratchpad"
        and resolved.parents[2].name == slug
        and resolved.parents[3].name.startswith("claude-")
        and any(resolved.is_relative_to(root) for root in temp_roots)
    )
    if in_workspace or in_claude_scratchpad:
        return resolved.read_text(errors="replace")
    return ""


def goal_text(prompt: str, cwd: str = "") -> str:
    stripped = prompt.strip()
    if stripped.startswith("/goal "):
        body = stripped[6:].strip()
    elif stripped.startswith("GOAL:"):
        body = stripped[5:].strip()
    else:
        return ""
    if not body or body.split()[0] in {"done", "clear", "consent", "revoke-consent"}:
        return ""
    return prepared_goal(body, cwd) or body


def route(prompt: str, harness: str, cwd: str = "") -> str:
    body = goal_text(prompt, cwd)
    if not body or not SKILL_NAMES:
        return ""
    manifest = json.loads((ROOT / ".agents/roles.json").read_text())
    skills = set(SKILL_PATTERN.findall(body.lower()))
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
    if re.search(r"\b(implement|fix|patch|code|apply|tighten|refactor|migrate)\b|\b(update|change|add|write|build)\s+(?:code|tests?|feature|schema|implementation)\b", lower):
        roles.append("develop-eshu")
    if "eshu-code-review" in skills or re.search(r"\b(review|pr.readiness)\b", lower):
        roles.append("review-eshu")
    if "golang-engineering" in skills and not roles:
        roles.append("develop-eshu")
    if not roles:
        roles.append("scan-eshu")
    # A named coordinator skill does not become a leaf-agent job.
    lines = ["Eshu goal role hints (phase candidates, not an execution order):"]
    if "eshu-issue-driver" in skills:
        lines.append("- eshu-issue-driver stays with the main coordinator for issue/PR ownership.")
    if "concurrency-deadlock-rigor" in skills:
        lines.append("- concurrency-deadlock-rigor is a method for the relevant worker, not a separate role.")
    bindings = manifest["models"].get(harness, {})
    for name in dict.fromkeys(roles):
        role = manifest["roles"][name]
        if "base" in role:
            role = {**manifest["roles"][role["base"]], **role}
        tier = role["tier"]
        binding = bindings.get(tier)
        model = f"; {binding['model']} effort={binding['effort']}" if binding else ""
        lines.append(f"- {name}: {role['description']} ({tier}; {role['access']}{model})")
    if harness == "claude":
        lines.append(
            "When delegating a bounded phase, select its named .claude/agents agent type "
            "and omit a model override unless the goal requests one. Team teammates "
            "must load named skills explicitly."
        )
    elif harness in {"codex", "muse"}:
        lines.append(f"When the native child tool cannot select this role and binding, the coordinator can run scripts/agent-roles.py {harness}-exec ROLE TASK.")
    lines.append("Preserve the goal's explicit phases and model choices. Do not assign the whole goal from this hint; the main session model is unchanged.")
    return "\n".join(lines)


def claude_dispatch(payload: dict) -> str:
    """Keep a named Eshu role's frontmatter model for an ordinary /goal run."""
    if payload.get("tool_name") != "Agent":
        return ""
    tool_input = payload.get("tool_input")
    if not isinstance(tool_input, dict) or not isinstance(tool_input.get("model"), str):
        return ""
    role_name = tool_input.get("subagent_type")
    if not isinstance(role_name, str):
        return ""
    manifest = json.loads((ROOT / ".agents/roles.json").read_text())
    role = manifest["roles"].get(role_name)
    if role is None:
        return ""
    if "base" in role:
        role = {**manifest["roles"][role["base"]], **role}
    expected = manifest["models"]["claude"][role["tier"]]["model"]
    if tool_input["model"] == expected:
        return ""

    cwd = payload.get("cwd")
    session_id = payload.get("session_id")
    if not isinstance(cwd, str) or not isinstance(session_id, str) or not cwd or not session_id:
        return ""
    safe_id = re.sub(r"[^A-Za-z0-9._-]", "-", session_id)
    goal_file = Path(cwd) / ".claude" / ("active-goal." + safe_id)
    if goal_file.is_symlink() or not goal_file.is_file() or goal_file.stat().st_size > 65536:
        return ""
    goal_lines = goal_file.read_text(errors="replace").splitlines()
    body_start = 0
    while body_start < len(goal_lines) and (
        not goal_lines[body_start].strip()
        or goal_lines[body_start].lstrip().lower().startswith("consent:")
    ):
        body_start += 1
    if body_start == len(goal_lines) or goal_lines[body_start] != "SESSION: " + session_id:
        return ""
    goal = "\n".join(goal_lines[body_start + 1:])
    if re.search(r"(?im)^\s*DONE\b", goal):
        return ""
    goal = prepared_goal(goal.strip(), cwd) or goal
    # A named model in the owner's goal takes precedence over repo defaults.
    if re.search(r"(?i)\b(?:haiku|sonnet|opus|fable|claude-(?:haiku|sonnet|opus|fable)[\w.-]*)\b", goal):
        return ""
    revised = {key: value for key, value in tool_input.items() if key != "model"}
    return json.dumps({"hookSpecificOutput": {
        "hookEventName": "PreToolUse",
        "updatedInput": revised,
        "additionalContext": "Eshu removed an unrequested model override for " + role_name
        + "; the role frontmatter selects " + expected + ".",
    }})


def main() -> None:
    try:
        payload = json.load(sys.stdin)
        harness = sys.argv[1] if len(sys.argv) > 1 else ""
        prompt = str(payload.get("prompt", ""))
        cwd = str(payload.get("cwd", ""))
        if harness == "claude-dispatch":
            result = claude_dispatch(payload)
            if result:
                print(result)
            return
        if harness == "expand":
            expanded = goal_text(prompt, cwd)
            if expanded:
                print(expanded)
            return
        context = route(prompt, harness, cwd)
        if context:
            print(json.dumps({"hookSpecificOutput": {"hookEventName": "UserPromptSubmit", "additionalContext": context}}))
    except (OSError, ValueError, TypeError, KeyError):
        # Prompt hooks must not prevent the user's message from being sent.
        return


if __name__ == "__main__":
    main()
