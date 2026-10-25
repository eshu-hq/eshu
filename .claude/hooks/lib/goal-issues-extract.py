#!/usr/bin/env python3
"""Reads a Bash PostToolUse payload and prints the issues the call created.

Output, one item per line, nothing at all when the call created no issue or the
payload is unusable: the session id, the cwd, then one `owner/repo#N` per issue.
issue-capture.sh reads that. The ledger itself is lib/goal-issues.py; this half
only decides WHICH tool calls count as creating an issue.

What counts:
  - a line that is exactly `https://github.com/<owner>/<repo>/issues/<N>` in the
    command's output -- what `gh issue create` prints, and what a script that
    files issues echoes. A gh READ verb (`issue list|view|status`, `search`, or
    a `gh api` GET) is excluded, because it prints URLs of OLD issues; the
    exception is a command that also runs `gh issue create`.
  - `html_url` in the JSON a `gh api repos/o/r/issues` POST returns.
"""
import json
import re
import sys

NAME = r"[A-Za-z0-9_.-]+"
URL_RE = re.compile(
    r"^https://github\.com/(" + NAME + r")/(" + NAME + r")/issues/([0-9]+)$"
)
READ_CMD_RE = re.compile(r"\bgh\s+(?:issue\s+(?:list|view|status)\b|search\b)")
ISSUE_CREATE_RE = re.compile(r"\bgh\s+issue\s+create\b")
API_RE = re.compile(r"\bgh\s+api\b")
API_ISSUES_ENDPOINT_RE = re.compile(
    r"""(?:^|[\s"'])/?repos/[^\s"']+/issues(?=$|[\s"'?])"""
)
API_WRITE_RE = re.compile(
    r"(?:(?:-X|--method)\s*=?\s*POST\b|(?:-f|-F|--field|--raw-field)\s*=?\s*title=)",
    re.IGNORECASE,
)
API_MUTATES_RE = re.compile(
    r"(?:(?:-X|--method)\s*=?\s*(?:POST|PUT|PATCH|DELETE)\b|"
    r"(?:-f|-F|--field|--raw-field|--input)\b)",
    re.IGNORECASE,
)



def ref_of(repo, number):
    """Returns the canonical `owner/repo#N` spelling."""
    return "%s#%s" % (repo, number)


def response_text(resp):
    """Flattens a tool_response into the text the command printed.

    Claude sends an object with `stdout`. Muse sends a JSON-ENCODED STRING whose
    object has `output` (recorded live; see scripts/testdata/issue-capture).
    A bare string and a list of content parts are handled too, because the
    capture must degrade to "nothing found" rather than raise.
    """
    if isinstance(resp, str):
        try:
            decoded = json.loads(resp)
        except ValueError:
            return resp
        return response_text(decoded) if isinstance(decoded, (dict, list)) else resp
    if isinstance(resp, dict):
        parts = [resp.get(k) for k in ("stdout", "output")]
        return "\n".join(p for p in parts if isinstance(p, str))
    if isinstance(resp, list):
        return "\n".join(response_text(p) for p in resp)
    return ""


def created_refs(command, text):
    """Returns the issues a Bash call created, in order, without duplicates."""
    refs = []

    def add(repo, number):
        r = ref_of(repo, number)
        if r not in refs:
            refs.append(r)

    api_write = bool(
        API_RE.search(command) and API_ISSUES_ENDPOINT_RE.search(command)
        and API_WRITE_RE.search(command)
    )
    api_read = bool(API_RE.search(command)) and not API_MUTATES_RE.search(command)
    # Bare `https://github.com/o/r/issues/N` on a line of its own is what
    # `gh issue create` prints, and what a script that files issues echoes. A
    # read verb prints URLs of OLD issues, so it is excluded -- unless the same
    # command also creates one, where the age check has to do the sorting.
    creates = bool(ISSUE_CREATE_RE.search(command))
    if creates or (not READ_CMD_RE.search(command) and not api_read):
        for line in text.splitlines():
            m = URL_RE.match(line.strip())
            if m:
                add(m.group(1) + "/" + m.group(2), m.group(3))
    if api_write:
        try:
            body = json.loads(text)
        except ValueError:
            body = None
        url = body.get("html_url") if isinstance(body, dict) else None
        m = URL_RE.match(url.strip()) if isinstance(url, str) else None
        if m:
            add(m.group(1) + "/" + m.group(2), m.group(3))
    return refs


def one_line(value):
    """Collapses a field to one line: the bash side reads these line by line."""
    return str(value or "").replace("\r", " ").replace("\n", " ")


def cmd_extract():
    """Prints sid, cwd, then one ref per created issue. Silent on bad input."""
    try:
        d = json.load(sys.stdin)
    except ValueError:
        return
    if not isinstance(d, dict):
        return
    tool_input = d.get("tool_input")
    command = tool_input.get("command") if isinstance(tool_input, dict) else None
    if not isinstance(command, str):
        return
    sid, cwd = one_line(d.get("session_id")), one_line(d.get("cwd"))
    if not sid or not cwd:
        return
    refs = created_refs(command, response_text(d.get("tool_response")))
    if not refs:
        return
    print(sid)
    print(cwd)
    for r in refs:
        print(r)


if __name__ == "__main__":
    try:
        cmd_extract()
    except Exception as exc:  # a hook must never be why a session breaks
        sys.stderr.write("goal-issues-extract: %s: %s\n" % (type(exc).__name__, exc))
    sys.exit(0)
