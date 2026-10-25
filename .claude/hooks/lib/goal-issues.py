#!/usr/bin/env python3
"""Reader and writer for the goal issue ledger (the `<goal file>.issues` sidecar).

The bash half is lib/goal-issues.sh, which resolves which ledger belongs to a
session. This half owns everything that touches ledger bytes or `gh`, so
issue-capture.sh, goal-continue.sh and goal-refresh.sh all read and write one
grammar through one implementation. See docs/internal/agent-hooks-issues.md.

Ledger grammar, one record per line:

    owner/repo#N <created-iso> <open|closed|deferred> [<ruling path>]
    DEFER: [owner/repo]#N <ruling path>        (written by the agent)

Payload extraction (what a Bash call created) lives in goal-issues-extract.py.

Subcommands (argv[1]); every one is fail-open and exits 0:

    capture LEDGER REF...   Confirm each ref is new via gh, then append it.
    gate LEDGER --cwd DIR --goal-file FILE [--state F --prior N --progress N --offset N]
                            The DONE check. Prints a Stop-hook block on stdout
                            when an issue is still open. The --state options carry
                            the consecutive-refusal count (see cmd_gate).
    list LEDGER             Prints the per-turn restatement, or nothing.
    merge SRC DEST          Fold the orphan ledger SRC into DEST, then drop SRC.

Environment: CLAUDE_GOAL_ISSUE_CALL_TIMEOUT (seconds per gh call, default 5) and
CLAUDE_GOAL_ISSUE_TOTAL_TIMEOUT (seconds for one whole check, default 30).
"""
import contextlib
import datetime
import fcntl
import json
import os
import re
import signal
import subprocess
import sys
import time

CALL_TIMEOUT = float(os.environ.get("CLAUDE_GOAL_ISSUE_CALL_TIMEOUT") or 5)
TOTAL_TIMEOUT = float(os.environ.get("CLAUDE_GOAL_ISSUE_TOTAL_TIMEOUT") or 30)
CAPTURE_TOTAL_TIMEOUT = min(TOTAL_TIMEOUT, 10.0)
NEW_ISSUE_WINDOW = datetime.timedelta(minutes=15)

NAME = r"[A-Za-z0-9_.-]+"
ENTRY_RE = re.compile(
    r"^(" + NAME + "/" + NAME + r")#([0-9]+)\s+(\S+)\s+(open|closed|deferred)"
    r"(?:\s+(.+?))?\s*$"
)
DEFER_RE = re.compile(
    r"^DEFER:\s*(?:(" + NAME + "/" + NAME + r"))?#([0-9]+)\s+(.+?)\s*$"
)


def ref_of(repo, number):
    """Returns the canonical `owner/repo#N` spelling."""
    return "%s#%s" % (repo, number)


def now_iso():
    """Returns the current UTC time in the ledger's timestamp format."""
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


@contextlib.contextmanager
def locked(ledger):
    """Holds an exclusive lock for one ledger across a read-modify-write.

    Capture hooks run from parallel subagents and a lead's Stop hook rewrites
    the same file, so an unlocked rewrite would silently drop a concurrent
    append. The lock file sits beside the ledger and is never read.
    """
    os.makedirs(os.path.dirname(os.path.abspath(ledger)), exist_ok=True)
    with open(ledger + ".lock", "a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        try:
            yield
        finally:
            fcntl.flock(lock, fcntl.LOCK_UN)


def read_lines(ledger):
    """Returns the ledger's lines, or [] when it is missing or unreadable."""
    try:
        with open(ledger, encoding="utf-8", errors="replace") as fh:
            return fh.read().splitlines()
    except OSError:
        return []


def write_lines(ledger, lines):
    """Replaces the ledger atomically, so a reader never sees half a rewrite."""
    tmp = "%s.tmp.%d" % (ledger, os.getpid())
    with open(tmp, "w", encoding="utf-8") as fh:
        fh.write("".join(line + "\n" for line in lines))
    os.replace(tmp, ledger)


def parse_entry(line):
    """Returns (ref, repo, number, created, state, path) or None."""
    m = ENTRY_RE.match(line.strip())
    if not m:
        return None
    repo, number, created, state, path = m.groups()
    return ref_of(repo, number), repo, number, created, state, path or ""


def entry_line(repo, number, created, state, path=""):
    """Formats one ledger record."""
    tail = " " + path if path else ""
    return "%s %s %s%s" % (ref_of(repo, number), created, state, tail)


def gh_json(args, budget):
    """Runs `gh <args>` and returns its parsed JSON, or None on any failure.

    The child gets its own process group so a hung gh and anything it spawned
    die together; otherwise a grandchild holding the pipe open would keep this
    hook alive past its timeout.
    """
    timeout = min(CALL_TIMEOUT, budget)
    if timeout <= 0:
        return None
    try:
        proc = subprocess.Popen(
            ["gh"] + args, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
            stdin=subprocess.DEVNULL, start_new_session=True,
        )
    except OSError:
        return None
    try:
        out, _ = proc.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        with contextlib.suppress(OSError):
            os.killpg(proc.pid, signal.SIGKILL)
        proc.communicate()
        return None
    if proc.returncode != 0:
        return None
    try:
        return json.loads(out.decode("utf-8", "replace"))
    except ValueError:
        return None


def split_ref(ref):
    """Splits `owner/repo#N` into (owner/repo, N)."""
    repo, _, number = ref.partition("#")
    return repo, number


def cmd_capture(ledger, refs):
    """Appends each new ref to the ledger and prints the ones it added."""
    start = time.monotonic()
    to_add = []
    for ref in refs:
        repo, number = split_ref(ref)
        created = now_iso()
        remaining = CAPTURE_TOTAL_TIMEOUT - (time.monotonic() - start)
        meta = gh_json(["issue", "view", number, "-R", repo, "--json", "createdAt"],
                       remaining)
        # gh down or slow: capture anyway. A spurious ledger line is cheap and
        # reversible; a missed issue is the exact leak this exists to stop.
        if isinstance(meta, dict) and isinstance(meta.get("createdAt"), str):
            try:
                made = datetime.datetime.strptime(
                    meta["createdAt"], "%Y-%m-%dT%H:%M:%SZ"
                ).replace(tzinfo=datetime.timezone.utc)
            except ValueError:
                made = None
            if made is not None:
                if datetime.datetime.now(datetime.timezone.utc) - made > NEW_ISSUE_WINDOW:
                    continue
                created = meta["createdAt"]
        to_add.append((repo, number, created))
    added = []
    if not to_add:
        return
    with locked(ledger):
        known = set()
        for line in read_lines(ledger):
            e = parse_entry(line)
            if e:
                known.add(e[0])
        records = []
        for repo, number, created in to_add:
            r = ref_of(repo, number)
            if r in known:
                continue
            known.add(r)
            records.append(entry_line(repo, number, created, "open") + "\n")
            added.append(r)
        if records:
            # One write() for the batch: O_APPEND keeps concurrent captures whole.
            with open(ledger, "a", encoding="utf-8") as fh:
                fh.write("".join(records))
    for r in added:
        print(r)


def same_file(a, b):
    """Reports whether two paths are one file (same device and inode)."""
    try:
        return os.path.samefile(a, b)
    except OSError:
        return False


def control_files(ledger, goal_file):
    """Returns the existing files a DEFER must never point at.

    The ledger, its lock, the goal file, every `active-goal*` file beside the
    ledger, and the ledger's temp files. Names are matched case-folded, because a
    case-insensitive volume (the macOS default) treats `ACTIVE-GOAL` as the same
    file, and identity (below) is what finally decides.
    """
    found = [ledger, ledger + ".lock"]
    if goal_file:
        found.append(goal_file)
    ledger_dir = os.path.dirname(os.path.abspath(ledger))
    ledger_name = os.path.basename(ledger).casefold()
    try:
        names = os.listdir(ledger_dir)
    except OSError:
        names = []
    for name in names:
        folded = name.casefold()
        if folded.startswith("active-goal") or folded.startswith(ledger_name + ".tmp."):
            found.append(os.path.join(ledger_dir, name))
    return [f for f in found if os.path.exists(f)]


def is_control_file(cand, ledger, goal_file):
    """Reports whether a DEFER path is the ledger, a goal file, or their scratch.

    Every ledger record contains `owner/repo#N`, and a goal file can name the
    issue too, so either would satisfy "names the issue" for free. Both are
    written by the agent, which makes them a self-issued permission slip. The
    test is file identity, not spelling: a symlink, a hardlink, `..`, or another
    letter case all reach the same file. A `.claude/active-goal*` name elsewhere
    (the per-user goal) is refused by name.
    """
    if any(same_file(cand, f) for f in control_files(ledger, goal_file)):
        return True
    real = os.path.realpath(cand)
    return (os.path.basename(os.path.dirname(real)).casefold() == ".claude"
            and os.path.basename(real).casefold().startswith("active-goal"))


def defer_problem(path, number, cwd, ledger, goal_file=""):
    """Returns why a DEFER path is not acceptable, or "" when it is.

    The agent writes the ledger, so a bare DEFER would be a self-issued
    permission slip. The path must exist, be non-empty, name the issue, and not
    be the ledger or a goal file (compared by file identity, so a symlink,
    hardlink, `..` or another letter case does not get around it).
    """
    roots = [cwd] if cwd and cwd != "-" else []
    ledger_dir = os.path.dirname(os.path.abspath(ledger))
    if os.path.basename(ledger_dir) == ".claude":
        roots.append(os.path.dirname(ledger_dir))
    expanded = os.path.expanduser(path)
    candidates = [expanded] if os.path.isabs(expanded) else [os.path.join(r, expanded) for r in roots]
    why = "no such file"
    for cand in candidates:
        if not os.path.isfile(cand):
            continue
        if is_control_file(cand, ledger, goal_file):
            why = "the ledger or a goal file is not a ruling"
            continue
        try:
            with open(cand, encoding="utf-8", errors="replace") as fh:
                body = fh.read(5_000_000)
        except OSError:
            why = "unreadable"
            continue
        if not body.strip():
            why = "file is empty"
        elif not re.search(r"#%s(?![0-9])" % re.escape(number), body):
            why = "file does not mention #%s" % number
        else:
            return ""
    return why


def apply_defers(ledger, cwd, goal_file=""):
    """Settles every DEFER line. Returns {ref: reason} for the refused ones."""
    refused = {}
    with locked(ledger):
        lines = read_lines(ledger)
        original = list(lines)
        entries = {}
        for i, line in enumerate(lines):
            e = parse_entry(line)
            if e:
                entries[i] = e
        drop = set()
        for i, line in enumerate(lines):
            m = DEFER_RE.match(line.strip())
            if not m:
                continue
            repo, number, path = m.groups()
            targets = [j for j, e in entries.items()
                       if e[2] == number and (repo is None or e[1] == repo) and e[4] == "open"]
            if not targets:
                continue
            problem = defer_problem(path, number, cwd, ledger, goal_file)
            for j in targets:
                _, r, n, created, _, _ = entries[j]
                if problem:
                    refused[ref_of(r, n)] = "DEFER path %s refused: %s" % (path, problem)
                else:
                    lines[j] = entry_line(r, n, created, "deferred", path)
                    entries[j] = parse_entry(lines[j])
                    # Echoed the way CONSENT is: the path is agent-writable, so
                    # the owner reads the exact words on stderr.
                    sys.stderr.write("goal-continue: DEFER honoured for %s: %s\n"
                                     % (ref_of(r, n), path))
            if not problem:
                drop.add(i)
        kept = [ln for i, ln in enumerate(lines) if i not in drop]
        if kept != original:
            write_lines(ledger, kept)
    return refused


ESCALATE_AFTER = 3
ESCALATE_TEXT = "Escalate to arbiter-eshu for the DEFER ruling now."


def forget_state(state):
    """Ends a run of refusals: the issues are cleared, so the count starts over."""
    if state:
        with contextlib.suppress(OSError):
            os.unlink(state)


def record_refusal(state, prior, progress, offset):
    """Returns this refusal's consecutive count and stores it in `state`.

    Tool calls since the previous refusal mean the agent is working, so the run
    starts over at 1. The file is the refusal's own: the goal's nudge counter is
    never touched, because that budget releases the stop and this refusal must
    never release open issues.
    """
    count = 1 if progress > 0 else prior + 1
    if state:
        with contextlib.suppress(OSError):
            with open(state, "w", encoding="utf-8") as fh:
                fh.write("%d %d" % (count, offset))
    return count


def int_opt(opts, key):
    """Returns a non-negative integer option, or 0 when absent or malformed."""
    try:
        return max(int(opts.get(key, 0)), 0)
    except (TypeError, ValueError):
        return 0


def cmd_gate(ledger, cwd, goal_file, opts=None):
    """The DONE check: block while a ledger issue is open; fail open on gh."""
    opts = opts or {}
    refusal_state = opts.get("--state", "")
    if not os.path.isfile(ledger):
        return
    refused = apply_defers(ledger, cwd, goal_file)
    pending = []
    for line in read_lines(ledger):
        e = parse_entry(line)
        if e and e[4] == "open":
            pending.append(e)
    if not pending:
        forget_state(refusal_state)
        return
    start = time.monotonic()
    still_open, unchecked, closed = [], [], []
    for e in pending:
        remaining = TOTAL_TIMEOUT - (time.monotonic() - start)
        meta = gh_json(["issue", "view", e[2], "-R", e[1], "--json", "state"], remaining)
        state = meta.get("state") if isinstance(meta, dict) else None
        if not isinstance(state, str):
            unchecked.append(e[0])
        elif state.upper() == "CLOSED":
            closed.append(e[0])
        else:
            still_open.append(e[0])
    if closed:
        with locked(ledger):
            lines = read_lines(ledger)
            for i, line in enumerate(lines):
                e = parse_entry(line)
                if e and e[0] in closed and e[4] == "open":
                    lines[i] = entry_line(e[1], e[2], e[3], "closed")
            write_lines(ledger, lines)
    if unchecked:
        sys.stderr.write("goal-continue: NOT_CHECKED issues: %s (gh unreachable or over the %gs cap; the ledger still lists them)\n"
                         % (", ".join(unchecked), TOTAL_TIMEOUT))
    if not still_open:
        if not unchecked:
            forget_state(refusal_state)
        return
    count = record_refusal(refusal_state, int_opt(opts, "--prior"), int_opt(opts, "--progress"),
                           int_opt(opts, "--offset"))
    numbers = ", ".join("#" + split_ref(r)[1] for r in still_open)
    first = "#" + split_ref(still_open[0])[1]
    lines = [
        "You are ending a goal that is marked DONE, but %d issue(s) filed during it are still open:"
        % len(still_open),
    ]
    for r in still_open:
        lines.append("  - %s%s" % (r, "  <- " + refused[r] if r in refused else ""))
    lines += [
        "",
        "An issue filed during a goal belongs to the goal; filing it is not a way to defer it. For each one, either:",
        "  1. work it now: dispatch a lane, land the fix, and close it; or",
        "  2. defer it on the record: add the line `DEFER: %s <path>` to %s, where <path> is an existing, non-empty file "
        "(a ruling, an arbiter verdict, an issue-comment export) that names the issue. A DEFER whose path fails that check is refused." % (first, ledger),
        "",
        "Open: %s. Then end the turn again." % numbers,
    ]
    if count >= ESCALATE_AFTER:
        lines += [
            "",
            "This is consecutive refusal %d with no tool calls in between. %s" % (count, ESCALATE_TEXT),
        ]
    print(json.dumps({"decision": "block", "reason": "\n".join(lines)}))
    sys.stderr.write("goal-continue: DONE refused (consecutive refusal %d); open issues: %s\n"
                     % (count, ", ".join(still_open)))


def cmd_list(ledger):
    """Prints the per-turn restatement for open issues, with no network."""
    open_refs = []
    for line in read_lines(ledger):
        e = parse_entry(line)
        if e and e[4] == "open":
            open_refs.append(e[0])
    if not open_refs:
        return
    first = "#" + split_ref(open_refs[0])[1]
    print("OPEN ISSUES FILED DURING THIS GOAL (ledger %s):" % ledger)
    for r in open_refs:
        print("- " + r)
    print(
        "Each one belongs to this goal: work it now (dispatch a lane), or add "
        "`DEFER: %s <ruling path>` to the ledger, where the path is an existing, "
        "non-empty file that names the issue. The Stop hook refuses DONE while "
        "any of these is open." % first
    )


def cmd_merge(src, dest):
    """Folds the orphan ledger into DEST, never duplicating a ref."""
    if not os.path.isfile(src):
        return
    first, second = sorted([src, dest])
    with locked(first), locked(second) if second != first else contextlib.nullcontext():
        incoming = read_lines(src)
        merged = read_lines(dest)
        known = {e[0] for e in (parse_entry(ln) for ln in merged) if e}
        for line in incoming:
            e = parse_entry(line)
            if e and e[0] not in known:
                known.add(e[0])
                merged.append(line.strip())
            elif not e and DEFER_RE.match(line.strip()):
                merged.append(line.strip())
        write_lines(dest, merged)
        with contextlib.suppress(OSError):
            os.unlink(src)


def main(argv):
    """Dispatches one subcommand; any failure is swallowed (exit 0)."""
    if len(argv) < 2:
        return
    cmd, args = argv[1], argv[2:]
    if cmd == "capture" and len(args) >= 2:
        cmd_capture(args[0], args[1:])
    elif cmd == "gate" and args:
        opts = dict(zip(args[1::2], args[2::2]))
        cmd_gate(args[0], opts.get("--cwd", ""), opts.get("--goal-file", ""), opts)
    elif cmd == "list" and args:
        cmd_list(args[0])
    elif cmd == "merge" and len(args) == 2:
        cmd_merge(args[0], args[1])


if __name__ == "__main__":
    try:
        main(sys.argv)
    except Exception as exc:  # a hook must never be why a session breaks
        sys.stderr.write("goal-issues: %s: %s\n" % (type(exc).__name__, exc))
    sys.exit(0)
