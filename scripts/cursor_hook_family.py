"""Helper-agent links, session keys and worktree roots for scripts/cursor-hook.py.

Claude Code gives a subagent its parent's session_id, so a skill the helper
loads is a skill the parent has loaded, and the reverse. Cursor runs a helper
(the Task tool) under its own id. So the adapter's subagentStart records a
child -> parent link next to the loaded-skill markers (which the .claude/hooks
scripts keep in /tmp), and the skill events follow the links to the root.

Only the skill events (preToolUse, postToolUse) use that family key. Claude
fires no Stop, UserPromptSubmit or SessionStart for a subagent, so goals and
compaction stay per chat: a helper never sees, retires or clears its parent's
goal or markers.

Every reader treats a bad state file (a symlink, a FIFO, not UTF-8, a bad id)
as no link at all, so the nudge keeps blocking rather than wrongly allowing.
Link and root files are never removed; ids are unique, so a stale one is
harmless. Documented in docs/internal/agent-hooks-cursor.md; pinned by
scripts/test-cursor-hooks-helper-cases.sh. Stdlib only, Python 3.9.
"""

import json
import os
import re
import stat
import subprocess
import sys
import tempfile
from typing import List, Optional, Tuple

STATE_DIR = "/tmp"
LINK_PREFIX = "eshu-cursor-link-"
ROOT_PREFIX = "eshu-cursor-root-"
LINK_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9._:-]{0,127}")
# The most links a chain may have: 16 resolves, 17 is too deep.
MAX_LINK_DEPTH = 16
ID_FIELDS = ("conversation_id", "session_id", "subagent_id")
LOG_FIELDS = ID_FIELDS + ("parent_conversation_id",)
LOG_ENV = "ESHU_CURSOR_HOOK_LOG"


def note(msg: str) -> None:
    """Write a note to stderr, which Cursor shows in its hook log."""
    sys.stderr.write("cursor-hook: " + msg.rstrip() + "\n")


def valid_id(v: object) -> bool:
    """A Cursor id that is safe to put in a state file name."""
    return isinstance(v, str) and bool(LINK_ID.fullmatch(v)) and ".." not in v


def chat_key(p: dict) -> object:
    """The per-chat key: conversation_id, else session_id (the old rule)."""
    return p.get("conversation_id") or p.get("session_id") or ""


def read_state(name: str) -> Optional[str]:
    """A state file's text, or None unless it is a readable regular file.

    O_NOFOLLOW refuses a symlink and O_NONBLOCK keeps a FIFO from hanging.
    """
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_NONBLOCK", 0)
    try:
        fd = os.open(os.path.join(STATE_DIR, name), flags)
    except OSError:
        return None
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            return None
        return os.read(fd, 256).decode("utf-8")
    except (OSError, ValueError):
        return None
    finally:
        os.close(fd)


def is_root(cid: str) -> bool:
    """Whether cid is marked as a family root (a regular file, not a link)."""
    try:
        return stat.S_ISREG(os.lstat(os.path.join(STATE_DIR, ROOT_PREFIX + cid)).st_mode)
    except OSError:
        return False


def link_parent(cid: str) -> Optional[str]:
    """The parent a helper id is linked to, or None."""
    text = read_state(LINK_PREFIX + cid)
    parent = text.strip() if text is not None else ""
    return parent if valid_id(parent) and parent != cid else None


def walk(cid: str) -> Tuple[List[str], str]:
    """cid and the ids its links lead to, root last, plus what went wrong:
    "" for a clean chain, "loop", or "too deep" (more than MAX_LINK_DEPTH)."""
    ids = [cid]
    while True:
        nxt = link_parent(ids[-1])
        if nxt is None:
            return ids, ""
        if nxt in ids:
            return ids, "loop"
        if len(ids) > MAX_LINK_DEPTH:
            return ids, "too deep"
        ids.append(nxt)


def family_key(p: dict) -> object:
    """The session_id the skill hooks get, shared by a helper family.

    The root of the first linked id (conversation_id, session_id, subagent_id),
    else an id that is a family root, else the per-chat key. A chain that
    loops or is too deep is ignored, so the id is used unlinked (fail safe).
    """
    cands = [p.get(k) for k in ID_FIELDS if valid_id(p.get(k))]
    for c in cands:
        ids, problem = walk(c)
        if problem:
            note("helper links from %s: %s; using the id unlinked" % (c, problem))
        elif len(ids) > 1:
            return ids[-1]
    for c in cands:
        if is_root(c):
            return c
    return chat_key(p)


def write_link(child: str, parent: str) -> None:
    """Link child to parent atomically, unless that would loop or be too deep."""
    up, problem = walk(parent)
    if problem == "loop":
        reason = "the parent's link chain loops"
    elif problem or len(up) > MAX_LINK_DEPTH:
        reason = "the parent's link chain is too deep"
    elif child in up:
        reason = "the link would loop"
    else:
        reason = ""
    if reason:
        note("not linking %s to %s: %s" % (child, parent, reason))
        return
    fd, tmp = tempfile.mkstemp(dir=STATE_DIR, prefix="." + LINK_PREFIX)
    try:
        with os.fdopen(fd, "w") as f:
            f.write(parent)
        os.replace(tmp, os.path.join(STATE_DIR, LINK_PREFIX + child))
    except OSError:
        os.unlink(tmp)
        raise
    flags = os.O_WRONLY | os.O_CREAT | getattr(os, "O_NOFOLLOW", 0)
    os.close(os.open(os.path.join(STATE_DIR, ROOT_PREFIX + up[-1]), flags, 0o600))


def record_links(p: dict) -> None:
    """subagentStart: link subagent_id, and a conversation_id or session_id
    that differs from the parent, to parent_conversation_id. In
    cursor-agent 2026.10.01 a session id belongs to one conversation, so
    linking the start's session_id cannot reach an unrelated chat (the
    Cursor IDE is not checked). Never raises OSError."""
    parent = p.get("parent_conversation_id")
    if not valid_id(parent):
        note("subagentStart has no usable parent_conversation_id; no link recorded")
        return
    for child in (p.get("subagent_id"), p.get("conversation_id"), p.get("session_id")):
        try:
            if valid_id(child) and child != parent:
                write_link(child, parent)
        except OSError as exc:
            note("subagentStart: could not record a helper link: %s" % exc)


def log_event(event: str, p: dict, key: object) -> None:
    """With ESHU_CURSOR_HOOK_LOG set to a file, append the event, the payload's
    key names, its ids and the key used. Never prompts, commands or paths."""
    path = os.environ.get(LOG_ENV)
    if not path:
        return
    row = {f: (v[:200] if isinstance(v, str) else None) for f, v in
           [(f, p.get(f)) for f in LOG_FIELDS] + [("key", key)]}
    row.update(event=event, keys=sorted(str(k) for k in p))
    try:
        with open(path, "a") as f:
            f.write(json.dumps(row, sort_keys=True) + "\n")
    except OSError as exc:
        note("payload log: %s" % exc)


def worktree_roots(project: str) -> List[str]:
    """Real paths of the worktrees sharing the project's git common dir; empty
    when git is missing or fails. GIT_* is dropped so GIT_DIR cannot steer it."""
    env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
    try:
        proc = subprocess.run(["git", "-C", project, "worktree", "list", "--porcelain"],
                              stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, env=env,
                              timeout=5, check=False)
    except (OSError, subprocess.SubprocessError) as exc:
        note("git worktree list failed (%s); only the project dir counts" % exc)
        return []
    if proc.returncode != 0:
        note("git worktree list exited %d; only the project dir counts" % proc.returncode)
        return []
    lines = proc.stdout.decode(errors="replace").splitlines()
    return [os.path.realpath(line[len("worktree "):]) for line in lines if line.startswith("worktree ")]
