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
        roles.append("develop-eshu-deep" if deep else "develop-eshu")
    if "eshu-code-review" in skills or re.search(r"\b(review|pr.readiness)\b", lower):
        roles.append("review-eshu-deep" if deep else "review-eshu")
    if re.search(r"\b(arbiter|arbitrat\w*|design[- ](?:call|fork)|waiver|ruling)\b", lower):
        roles.append("arbiter-eshu")
    if "golang-engineering" in skills and not roles:
        roles.append("develop-eshu")
    # A drive goal always needs implementation + independent review lanes,
    # even when its wording names neither ("drive epics #N to completion").
    if "eshu-issue-driver" in skills or re.search(r"\bdrive\b.*\b(epic|issue|#\d+)", body.lower()):
        for needed in ("develop-eshu-deep" if deep else "develop-eshu",
                       "review-eshu-deep" if deep else "review-eshu"):
            if needed not in roles:
                roles.append(needed)
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
    # Drive contract: "/goal drive epics #N, #M using eshu-issue-driver" (or
    # the same shape in plain words) must self-organize without the owner
    # also prompting for subagents/workflows. The coordinator keeps issue/PR
    # ownership; leaves fan out to leaf roles. Fan-out is harness-native:
    # Workflow on Muse only; Claude uses its native subagents / team
    # teammates (never Workflow); other harnesses use their native subagent
    # tool. This is injected context the model must follow per AGENTS.md —
    # a hook cannot force the tool call itself.
    drive = "eshu-issue-driver" in skills or bool(
        re.search(r"\bdrive\b.*\b(epic|issue|#\d+)", body.lower())
    )
    if drive:
        if harness == "muse":
            fanout = "a Workflow (parallel/pipeline over leaves) or subagent_spawn"
        elif harness == "claude":
            fanout = "native subagents / team teammates (Agent tool; teammates load named skills explicitly) — not Workflow"
        else:
            fanout = "native subagents in parallel"
        lines.append(
            "DRIVE CONTRACT (no further prompting needed): the coordinator loads "
            "eshu-issue-driver now and owns epic/issue/PR state end to end. "
            "Decompose each epic into deduplicated leaves, present the numbered "
            "execution order, then drive every leaf to merged/closed: worktree "
            "per leaf, minimum skills per touched surface, TDD + focused proof, "
            "independent review-eshu review, promotion (only the coordinator "
            "runs make pre-push / pushes / merges). Independent leaves MUST fan "
            f"out via {fanout} to the routed *-eshu roles above (scan for "
            "evidence, debug for unknown cause, develop for implementation, "
            "perf for measurements, review for the verdict, arbiter-eshu for "
            "unsettled decisions) — commit before dispatch, one surface per "
            "dispatch, re-verify every child finding before acting on it. "
            "Sequential-only work (shared files, ordered dependencies) stays "
            "coordinator-driven; record that call in the goal file. After "
            "every gh pr create, arm the CI watcher in the same turn. Every "
            "issue created during the drive joins the active backlog (goal "
            "inventory) with an existing label and its epic linked when one "
            "exists. Do not end the goal while any leaf, PR, review thread, "
            "or session-created issue is open without a recorded disposition."
        )
    if harness == "muse":
        # Muse shims' model/effort are inert for spawned children, and Muse's
        # built-in posture prefers inline work. Project rules override both:
        # dispatch is required on multi-lane work, with manifest bindings
        # passed explicitly on every call. See
        # docs/internal/agent-orchestration.md (Muse Code row).
        lines.append(
            "Muse REQUIRED (project rules override built-in inline preference): "
            "load each routed skill body with read_skill before acting on its surface; "
            "when the goal has two or more independent lanes (implementation + review, "
            "parallel issues, scan/debug/perf splits), dispatch the routed *-eshu roles "
            "via subagent_spawn/workflow — commit first, pass the model+effort above "
            "explicitly on every call (shim frontmatter is inert on Muse), or record "
            "in the goal file why inline is correct. Single-lane work stays inline. "
            "After every gh pr create, arm the CI Monitor watcher in the same turn "
            "(GHA failures, merge conflicts, any-author reviews/comments). Never "
            "request a Copilot review. Every gh issue create joins the active "
            "backlog (goal inventory) with an existing label and its epic linked "
            "when one exists, in the same turn."
        )
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
