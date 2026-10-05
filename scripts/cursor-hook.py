#!/usr/bin/env python3
"""Run an Eshu Claude Code hook under Cursor's native hooks.

Usage (from .cursor/hooks.json, which Cursor runs from the project root):

    python3 scripts/cursor-hook.py <cursor-event> <hook-script> [args...]

Cursor and Claude Code send different JSON to a hook and read different JSON
back. This adapter is the only Cursor-specific code: it turns the Cursor
payload into the Claude shape, runs the unchanged script under .claude/hooks/,
and turns the Claude answer back into the Cursor shape. The hook scripts stay
the single copy of the logic. Helper-agent links and worktree roots live in
scripts/cursor_hook_family.py. Mapping and limits are documented in
docs/internal/agent-hooks-cursor.md; scripts/test-cursor-hooks.sh pins them.

Rules that matter (from https://cursor.com/docs/hooks):

- A permission event must print exactly one JSON object. Cursor treats
  invalid JSON from a permission hook as a block, so this always prints one.
- Guard events (anything that can refuse an action) fail CLOSED when the
  adapter or the hook breaks: a broken guard must not silently allow.
- Advisory events fail OPEN with a note on stderr.

Stdlib only, no network, and Python 3.9 compatible: Cursor runs whatever
python3 is first on its PATH, which is 3.9 on a stock macOS.
"""

import json
import os
import re
import subprocess
import sys
from pathlib import Path
from typing import List, Optional, Tuple

REPO_ROOT = Path(__file__).resolve().parent.parent

# Helper-agent links and worktree roots live in a sibling module. If it cannot
# load (missing, or broken: a syntax error or a raise at import), main() fails
# closed for a guard and open for anything else.
sys.path.insert(0, str(Path(__file__).resolve().parent))
try:
    import cursor_hook_family as family
    FAMILY_ERROR = ""
except Exception as _exc:  # noqa: BLE001 - any import failure must reach main()
    family = None  # type: ignore[assignment]
    FAMILY_ERROR = "cannot import cursor_hook_family: %s" % _exc

# Events whose output is a permission decision. Every one is a guard.
PERMISSION_EVENTS = {"beforeShellExecution", "preToolUse"}
# subagentStart runs no hook script. The adapter records the helper link and
# always answers allow: it is advisory and must never block a helper.
SUBAGENT_START = "subagentStart"
# Only these events use the helper-family key (Claude shares a subagent's
# loaded skills with its parent, but fires no goal or SessionStart hook for it).
FAMILY_EVENTS = {"preToolUse", "postToolUse"}
ADVISORY_EVENTS = {
    "afterFileEdit",
    "postToolUse",
    "sessionStart",
    "preCompact",
    "beforeSubmitPrompt",
    "stop",
}

# cursor-agent 2026.10.01 sends Write as {file_path, content}. The other keys
# are fallbacks, the same list the fleet-board and agent-config adapters read.
# A present key that is not a string is a malformed payload (guards deny); no
# key at all is allowed with a note, rather than blocking on a guessed path.
PATH_KEYS = ("file_path", "path", "target_file", "filePath", "file")

# A skill is loaded in Cursor by reading its SKILL.md (there is no Skill tool).
# Only the project's skill folders (or a git worktree's of the same repo) count.
SKILL_DIRS = (".agents/skills", ".claude/skills", ".codex/skills", ".cursor/skills")
SKILL_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]*")

# Longest message built from a hook's output that is passed on to Cursor.
MESSAGE_LIMIT = 20000

def cursor_skill_note() -> str:
    """How a Cursor agent loads a skill; a git worktree's copy counts too."""
    path = os.path.join(project_dir(), ".agents", "skills", "<id>", "SKILL.md")
    return (
        "In Cursor there is no Skill tool: load a skill by reading "
        + path + " with the Read tool (the copy in the git worktree you are"
        " editing counts too). The postToolUse hook records that read."
    )


class BadPayload(ValueError):
    """The Cursor payload has a field of the wrong type."""


def clip(text: str) -> str:
    """Cut a message built from hook output to MESSAGE_LIMIT characters."""
    if len(text) <= MESSAGE_LIMIT:
        return text
    return text[:MESSAGE_LIMIT] + "\n... [truncated %d characters]" % (len(text) - MESSAGE_LIMIT)


def note(msg: str) -> None:
    """Write an adapter note to stderr, which Cursor shows in its hook log."""
    sys.stderr.write("cursor-hook: " + msg.rstrip() + "\n")


def emit(obj: dict, code: int = 0) -> None:
    """Print exactly one JSON object and exit."""
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()
    sys.exit(code)


def deny(msg: str) -> dict:
    return {"permission": "deny", "user_message": msg, "agent_message": msg}


def fail(event: str, msg: str) -> None:
    """Fail closed for a guard, open for anything else."""
    note(msg)
    if event == SUBAGENT_START:
        emit({"permission": "allow"})
    if event == "beforeSubmitPrompt":
        emit({"continue": True})
    if event in ADVISORY_EVENTS:
        emit({})
    emit(deny("Eshu Cursor hook failed closed: " + msg))


def project_dir() -> str:
    return os.environ.get("CURSOR_PROJECT_DIR") or os.environ.get("CLAUDE_PROJECT_DIR") or str(REPO_ROOT)


def payload_cwd(p: dict) -> str:
    """cwd for the hook: payload cwd, else first workspace root, else project dir.

    goal-refresh.sh writes <cwd>/.claude/active-goal.<id> and goal-continue.sh
    reads it back, so every event must resolve cwd the same way.
    """
    cwd = p.get("cwd")
    if isinstance(cwd, str) and cwd:
        return cwd
    roots = p.get("workspace_roots")
    if isinstance(roots, list) and roots and isinstance(roots[0], str) and roots[0]:
        return roots[0]
    return project_dir()


def find_path(p: dict, cwd: str) -> Optional[str]:
    """The first present path key, joined onto cwd; None when there is none.

    Raises BadPayload when a present key holds something other than a string.
    """
    ti = p.get("tool_input")
    candidates = [("tool_input." + k, ti.get(k)) for k in PATH_KEYS] if isinstance(ti, dict) else []
    candidates.append(("file_path", p.get("file_path")))
    for key, c in candidates:
        if c is None:
            continue
        if not isinstance(c, str):
            raise BadPayload("%s is a %s, not a string" % (key, type(c).__name__))
        if c:
            return c if os.path.isabs(c) else os.path.join(cwd, c)
    return None


def skill_id(path: str) -> Optional[str]:
    """The project skill a Read of this SKILL.md loads, or None.

    The real path (symlinks and ../ resolved) must sit directly in a skill
    folder of the project, or of another git worktree of the same repository,
    and the id must be a skill under that same tree's .agents/skills.
    """
    real = os.path.realpath(path)
    if os.path.basename(real) != "SKILL.md":
        return None
    folder = os.path.dirname(real)
    sid = os.path.basename(folder)
    if not SKILL_ID.fullmatch(sid):
        return None

    def holds(root: str) -> bool:
        return any(os.path.dirname(folder) == os.path.realpath(os.path.join(root, d)) for d in SKILL_DIRS)

    root = os.path.realpath(project_dir())
    if not holds(root):
        root = next((w for w in family.worktree_roots(root) if holds(w)), "")
        if not root:
            return None
    if not os.path.isfile(os.path.join(root, ".agents", "skills", sid, "SKILL.md")):
        return None
    return sid


def translate(event: str, p: dict, key: object) -> Tuple[Optional[dict], Optional[dict]]:
    """Return (claude_payload, early_answer). One of the two is None."""
    cwd = payload_cwd(p)
    base = {
        "session_id": key,
        "cwd": cwd,
        "transcript_path": p.get("transcript_path"),
    }
    if event == "beforeShellExecution":
        cmd = p.get("command")
        if not isinstance(cmd, str):
            return None, deny("Eshu Cursor hook failed closed: no command in the payload")
        return dict(base, hook_event_name="PreToolUse", tool_name="Bash", tool_input={"command": cmd}), None
    if event == "preToolUse":
        tool = p.get("tool_name") or ""
        if tool == "Shell":
            ti = p.get("tool_input") if isinstance(p.get("tool_input"), dict) else {}
            return dict(base, hook_event_name="PreToolUse", tool_name="Bash", tool_input=ti), None
        path = find_path(p, cwd)
        if path is None:
            note("no file path found in the %s payload (tried tool_input.%s and file_path); allowing rather than blocking on a guess"
                 % (tool or "tool", ", tool_input.".join(PATH_KEYS)))
            return None, {"permission": "allow"}
        return dict(base, hook_event_name="PreToolUse", tool_name="Write", tool_input={"file_path": path}), None
    if event == "postToolUse":
        try:
            path = find_path(p, cwd) if p.get("tool_name") == "Read" else None
        except BadPayload as exc:
            note("ignoring Read payload: %s" % exc)
            return None, {}
        sid = skill_id(path) if path else None
        if not sid:
            return None, {}
        return dict(base, hook_event_name="PostToolUse", tool_name="Skill", tool_input={"skill": sid}), None
    if event == "afterFileEdit":
        try:
            edited = find_path(p, cwd) or ""
        except BadPayload as exc:
            note("afterFileEdit payload: %s" % exc)
            edited = ""
        return dict(base, hook_event_name="PostToolUse", tool_name="Edit", tool_input={"file_path": edited}), None
    if event == "sessionStart":
        return dict(base, hook_event_name="SessionStart", source="startup"), None
    if event == "preCompact":
        return dict(base, hook_event_name="SessionStart", source="compact"), None
    if event == "beforeSubmitPrompt":
        return dict(base, hook_event_name="UserPromptSubmit", prompt=p.get("prompt") or "",
                    prompt_id=p.get("generation_id") or ""), None
    if event == "stop":
        if p.get("status") != "completed":
            # Claude fires no Stop hook on a user interrupt or an error, so
            # only a stop Cursor reports as completed becomes a follow-up. A
            # missing or empty status is not taken as completed.
            return None, {}
        loops = p.get("loop_count")
        return dict(base, hook_event_name="Stop", prompt_id=p.get("generation_id") or "",
                    stop_hook_active=isinstance(loops, int) and loops > 0), None
    return None, None


def run_hook(script: str, args: List[str], claude_payload: dict) -> Tuple[int, str, str]:
    path = Path(script)
    if not path.is_absolute():
        path = REPO_ROOT / path
    if not path.is_file():
        raise FileNotFoundError("hook script not found: %s" % path)
    argv = [str(path)] + args
    if path.suffix == ".sh":
        argv = ["bash"] + argv
    elif path.suffix == ".py":
        argv = [sys.executable] + argv
    env = dict(os.environ)
    env.setdefault("CLAUDE_PROJECT_DIR", project_dir())
    proc = subprocess.run(argv, input=json.dumps(claude_payload).encode(), stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, env=env, check=False)
    return proc.returncode, proc.stdout.decode(errors="replace"), proc.stderr.decode(errors="replace")


def parse_claude(stdout: str) -> Tuple[bool, str, str]:
    """Return (blocked, reason, context) from a Claude hook's stdout."""
    text = stdout.strip()
    if not text:
        return False, "", ""
    try:
        d = json.loads(text)
    except ValueError:
        return False, "", text
    if not isinstance(d, dict):
        return False, "", text
    hso = d.get("hookSpecificOutput") if isinstance(d.get("hookSpecificOutput"), dict) else {}
    blocked = d.get("decision") == "block" or hso.get("permissionDecision") == "deny"
    reason = d.get("reason") or hso.get("permissionDecisionReason") or ""
    context = hso.get("additionalContext") or ""
    return blocked, str(reason), str(context)


def answer(event: str, script: str, rc: int, stdout: str, stderr: str) -> Tuple[dict, int]:
    msg = clip(stderr.strip())
    if rc == 2:
        msg = msg or ("%s refused the action" % Path(script).name)
        if event in PERMISSION_EVENTS:
            out = deny(msg)
            if Path(script).name == "skill-nudge.sh":
                out["agent_message"] = msg + "\n" + cursor_skill_note()
            return out, 0
        if event == "stop":
            return {"followup_message": msg}, 0
        if event == "beforeSubmitPrompt":
            return {"continue": False, "user_message": msg}, 0
        note(msg)
        return {}, 2
    if rc != 0:
        detail = "%s exited %d%s" % (Path(script).name, rc, (": " + msg) if msg else "")
        if event in PERMISSION_EVENTS:
            return deny("Eshu Cursor hook failed closed: " + detail), 0
        note(detail + " (advisory hook, failing open)")
        return {}, 0
    if msg:
        note(msg)
    blocked, reason, context = parse_claude(stdout)
    reason, context = clip(reason), clip(context)
    if event in PERMISSION_EVENTS:
        if blocked:
            return deny(reason or "%s refused the action" % Path(script).name), 0
        return ({"permission": "allow", "agent_message": context} if context else {"permission": "allow"}), 0
    if event == "stop":
        return ({"followup_message": reason or context} if blocked and (reason or context) else {}), 0
    if event == "beforeSubmitPrompt":
        # Cursor shows user_message only when continue is false, and its docs
        # list no field that adds context to the prompt, so a goal restated by
        # goal-refresh.sh is dropped here. Its side effect (the goal file) stays.
        if blocked:
            return {"continue": False, "user_message": reason}, 0
        return {"continue": True}, 0
    if event == "preCompact":
        return ({"user_message": context + "\n" + cursor_skill_note()} if context else {}), 0
    if event in ("sessionStart", "postToolUse"):
        return ({"additional_context": context} if context else {}), 0
    return {}, 0


def main(argv: List[str]) -> None:
    event = argv[1] if len(argv) > 1 else ""
    if len(argv) < 3 and event != SUBAGENT_START:
        fail("", "usage: cursor-hook.py <cursor-event> <hook-script> [args...]")
    if event not in PERMISSION_EVENTS and event not in ADVISORY_EVENTS and event != SUBAGENT_START:
        fail(event, "unknown Cursor event %r" % event)
    if family is None:
        fail(event, FAMILY_ERROR)
    raw = sys.stdin.read()
    try:
        if not raw.strip():
            raise ValueError("empty payload")
        payload = json.loads(raw)
        if not isinstance(payload, dict):
            raise ValueError("payload is not a JSON object")
    except ValueError as exc:
        fail(event, "unreadable %s payload: %s" % (event, exc))
        return
    seen = payload.get("hook_event_name")
    if seen and seen != event:
        note("payload says %s but hooks.json wired %s; using %s" % (seen, event, event))
    key = family.family_key(payload) if event in FAMILY_EVENTS else family.chat_key(payload)
    family.log_event(event, payload, key)
    if event == SUBAGENT_START:
        family.record_links(payload)
        emit({"permission": "allow"})
    script, args = argv[2], argv[3:]
    try:
        claude_payload, early = translate(event, payload, key)
    except BadPayload as exc:
        fail(event, "malformed %s payload: %s" % (event, exc))
        return
    if early is not None:
        emit(early)
    if claude_payload is None:
        fail(event, "no translation for %s" % event)
        return
    try:
        rc, out, err = run_hook(script, args, claude_payload)
    except (OSError, ValueError) as exc:
        fail(event, str(exc))
        return
    result, code = answer(event, script, rc, out, err)
    emit(result, code)


if __name__ == "__main__":
    try:
        main(sys.argv)
    except SystemExit:
        raise
    except Exception as exc:  # pylint: disable=broad-except
        # Last resort: one JSON object, closed for guards.
        ev = sys.argv[1] if len(sys.argv) > 1 else ""
        fail(ev, "adapter error: %s: %s" % (type(exc).__name__, exc))
