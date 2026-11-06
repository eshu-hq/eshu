"""Text scanners behind scripts/verify-nornicdb-ci-off.py.

The guard decides which jobs are live and which registry rows are manual. This
module answers a narrower question about one body of text: does it start a
NornicDB backend? A body is a live job's lines, a composite action's
``action.yml``, or a script a job runs. Everything is derived from the
committed files; no file name is listed here.

What a start is (the guard's header lists the same four reads and the stated limits):

* a workflow or action line that selects ``nornicdb`` (matrix value, env key,
  ``--backend nornicdb``, an expression that picks it for a backend key),
  brings up the compose service or profile, ``docker run``s an image, or
  assigns ``ESHU_GRAPH_BACKEND=nornicdb`` inside a command; a job ``services:``
  or ``container:`` image that names NornicDB; names are matched ignoring case;
* the same inside a shell or Python script the body runs, followed through
  every script it runs in turn (cycle-safe, no depth cap);
* the same inside a local composite action (``uses: ./.github/actions/<name>``)
  the job calls, and inside any action that one calls.

Shell text is read after ``\\`` continuations are joined, and a shell variable
assigned a literal earlier in the same body (the same step in a workflow) is
substituted into later statements, so ``svc=nornicdb`` then ``up -d "$svc"``
reads as ``up -d nornicdb``. A value computed at run time (``$(...)``, an
argument, a default) is not resolved; that stays a stated limit. A script
statement is read by ``nornicdb_ci_off_shell``, which splits a line at ``&&``,
``;``, ``|`` and ``then`` outside quotes, drops ``sudo``, ``timeout N`` and the
like, and reads a ``bash -c '...'`` body, so a start behind those still counts.
"""

from __future__ import annotations

import ast
import os
import re
from dataclasses import dataclass
from pathlib import Path

import nornicdb_ci_off_shell as shell

OFF_NOTE = "NornicDB CI is off by owner order (2026-11-05)"
BACKEND_ENV = "ESHU_GRAPH_BACKEND"
MATRIX_VALUE = "lists nornicdb as a matrix value"
_EXPRESSION_BACKEND = (r"\b(?:ESHU_GRAPH_BACKEND|graph_backend|graph_service|backend):\s*"
                       r"\$\{\{[^\n]*\bnornicdb\b")
_DOCKER_RUN = r"(?:docker(?:\s+container)?|podman)\s+run"
_COMPOSE_FILE_UP = (r"\b(?:docker|podman)[ -]compose\b[^\n]*(?:-f|--file)[ =][\"']?\S*nornicdb"
                    r"\S*[^\n]*\bup\b")
_PROFILES_BODY = r"COMPOSE_PROFILES(?:=|:\s*)[\"']?[^\"'\n]*\bnornicdb\b"
_PROFILES_ENV = r"\b" + _PROFILES_BODY
_PROFILE_UP = r"^(?=.*\b(?:up|run)\b).*--profile[ =][\"']?nornicdb\b"
START_MARKERS = tuple((re.compile(pattern, re.IGNORECASE), reason) for pattern, reason in (
    (r"graph_backend:\s*\[?[^\n]*\bnornicdb\b", "selects the nornicdb graph backend"),
    (r"graph_service:\s*[\"']?nornicdb\b", "selects the nornicdb compose service"),
    (rf"{BACKEND_ENV}:\s*[\"']?nornicdb\b", f"sets {BACKEND_ENV} to nornicdb"),
    (_EXPRESSION_BACKEND, "selects the nornicdb backend through an expression"),
    (rf"\b{BACKEND_ENV}=[\"']?nornicdb\b", f"assigns {BACKEND_ENV}=nornicdb in a run command"),
    (r"--backend[ =][\"']?nornicdb\b", "runs a runner with --backend nornicdb"),
    (r"\bup\b[^\n]*\bnornicdb\b", "brings up the nornicdb compose service"),
    (_COMPOSE_FILE_UP, "brings up the nornicdb compose service"),
    (_PROFILE_UP, "brings up the nornicdb compose profile"),
    (_PROFILES_ENV, "enables the nornicdb compose profile"),
    (rf"\b{_DOCKER_RUN}\b[^\n]*nornicdb", "docker-runs a NornicDB image"),
    (r"^\s*(?:-\s+)?uses:\s*[\"']?docker://\S*nornicdb", "runs a NornicDB image as a step"),
    (r"^\s*(?:-\s*)?\w+:\s*\[[^\]]*\bnornicdb\b", MATRIX_VALUE),
    (r"^\s*-\s*\w+:\s*[\"']?nornicdb[\"']?\s*$", MATRIX_VALUE),
))
# A step ``name:`` is a label. The reasons below read a command, so a name such
# as "Clean up nornicdb volumes" must not trip them; the config-style reasons
# (a backend key, a matrix value) still apply to a name line.
NAME_LINE = re.compile(r"^\s*(?:-\s+)?name:\s")
RUN_ONLY_REASONS = frozenset((
    "brings up the nornicdb compose service", "brings up the nornicdb compose profile",
    "docker-runs a NornicDB image"))
# A job ``services:`` or ``container:`` names the image GitHub starts for the job.
JOB_CONTAINER = re.compile(r"^(\s*)(?:services|container):\s*(.*)$")
JOB_IMAGE = re.compile(r"^\s*(?:-\s+)?image:\s*[\"']?([^\s\"']+)")
# The script a statement runs: its first word after env assignments and an
# interpreter, so a script merely named in a string or an argument is not
# followed. A leading ``${VAR}/`` or ``${{ expr }}/`` (``github.action_path``)
# is part of the word and is dropped when the path is resolved.
RUNS_SCRIPT = re.compile(
    r"^(?:-\s+)?(?:run:\s*)?(?:\w+=[^\s\"'$()]+\s+)*"
    r"(?:(?:bash|sh|source|exec|python3?|\.)\s+)?(?:-[\w-]+\s+)*[\"']?"
    r"((?:(?:\$\{?\w+\}?|\$\{\{[^}]*\}\})/)?[A-Za-z0-9_./-]+\.(?:sh|py))[\"']?(?:\s|$)")
SCRIPT_DOCKER_RUN = re.compile(rf"^{_DOCKER_RUN}\b.*nornicdb", re.IGNORECASE)
# A shell statement starts a backend only when the statement itself is the
# command. A line that merely quotes the command (an assertion in a self-test,
# a regex, a usage string) starts with something else and does not match.
_LEAD = r"^(?:\w+=[^\s\"'$()]+\s+)*(?:bash\s+|sh\s+|\./)?"
SCRIPT_START_MARKERS = tuple((re.compile(pattern, re.IGNORECASE), reason) for pattern, reason in (
    (_LEAD + r"(?:\S*compose\.sh|docker[ -]compose)\b.*\bup\b.*\bnornicdb\b",
     "brings up the nornicdb compose service"),
    (_LEAD + r"(?:\S*compose\.sh|docker[ -]compose)\b(?=.*\b(?:up|run)\b).*"
     r"--profile[ =][\"']?nornicdb\b", "brings up the nornicdb compose profile"),
    (_LEAD + r"(?:\S*compose\.sh|(?:docker|podman)[ -]compose)\b.*(?:-f|--file)[ =][\"']?"
     r"\S*nornicdb\S*.*\bup\b", "brings up the nornicdb compose service"),
    (_LEAD + r"\S*scripts/\S+\s.*--backend[ =][\"']?nornicdb\b",
     "runs a runner with --backend nornicdb"),
    (rf"^(?:export\s+|declare\s+-x\s+)?(?:\w+=[^\s\"'$()]+\s+)*"
     rf"{BACKEND_ENV}=[\"']?nornicdb\b", f"sets {BACKEND_ENV} to nornicdb"),
    (r"^(?:export\s+|declare\s+-x\s+)?(?:\w+=[^\s\"'$()]+\s+)*" + _PROFILES_BODY,
     "enables the nornicdb compose profile"),
))
PY_SPAWNERS = {"run", "Popen", "call", "check_call", "check_output", "system", "popen"}
PY_COMMAND_START = re.compile(
    rf"\b{_DOCKER_RUN}\b.*nornicdb|\bdocker[ -]compose\b.*\bup\b.*\bnornicdb\b", re.IGNORECASE)
LOCAL_ACTION = re.compile(
    r"^\s*(?:-\s+)?uses:\s*[\"']?\./\.github/actions/([A-Za-z0-9_./-]+?)/?[\"']?\s*$")
# A new workflow step is a new shell, so literals do not carry across it.
STEP_START = re.compile(r"^\s*-\s+[\w-]+:")
ASSIGNMENT = re.compile(
    r"^\s*(?:export\s+|readonly\s+|declare\s+(?:-\w+\s+)?|local\s+)?([A-Za-z_]\w*)=(.*)$")
VARIABLE = re.compile(r"\$\{(\w+)\}|\$(\w+)")

Numbered = list[tuple[int, str]]


class ReadError(Exception):
    """A file the guard must read could not be opened or decoded as UTF-8."""


def read_text(path: Path) -> str:
    """Return a file's text, or raise ``ReadError`` naming the file.

    A file the guard cannot read must fail the gate with a clear message, not
    end it in a traceback and not be skipped.
    """
    try:
        return path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as err:
        raise ReadError(f"cannot read {path}: {type(err).__name__}") from err


@dataclass(frozen=True)
class Unit:
    """One body of workflow text: a job's lines or a composite action's file.

    Attributes:
        label: Repo-relative path of the file the lines came from.
        lines: Active (non-comment) lines with their 1-based line numbers.
        base: Directory a script named in this unit may also resolve against.
        subject: Words naming the unit in a message ("a live job", ...).
    """

    label: str
    lines: Numbered
    base: Path | None
    subject: str


def numbered_active(text: str) -> Numbered:
    """Return non-comment lines with trailing YAML comments removed and line numbers."""
    lines = []
    for number, line in enumerate(text.splitlines(), 1):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        lines.append((number, re.sub(r"\s+#.*$", "", line.rstrip())))
    return lines


def join_continuations(lines: Numbered) -> Numbered:
    """Join each ``\\``-continued line to the next; the statement keeps its first line number."""
    joined: Numbered = []
    pending: tuple[int, str] | None = None
    for number, line in lines:
        if pending is not None:
            number, line = pending[0], pending[1] + " " + line.lstrip()
            pending = None
        if line.endswith("\\"):
            pending = (number, line[:-1].rstrip())
            continue
        joined.append((number, line))
    if pending is not None:
        joined.append(pending)
    return joined


def literal_value(raw: str) -> str | None:
    """Return the value of a shell assignment when it is a plain literal, else None."""
    raw = raw.strip()
    for quote in ('"', "'"):
        if len(raw) >= 2 and raw[0] == quote and raw[-1] == quote:
            inner = raw[1:-1]
            return None if re.search(r"[$`\\]|" + quote, inner) else inner
    return raw if re.fullmatch(r"[^\s$`\"'\\;&|()<>]*", raw) else None


def expand_literals(statements: Numbered, split_steps: bool) -> Numbered:
    """Substitute variables assigned a literal earlier in the same body.

    A later assignment that is not a literal forgets the name. With
    ``split_steps`` a workflow step boundary forgets every name, because each
    step runs in its own shell.
    """
    known: dict[str, str] = {}
    expanded: Numbered = []
    for number, statement in statements:
        if split_steps and STEP_START.match(statement):
            known.clear()
        if "$" in statement:
            statement = VARIABLE.sub(
                lambda m: known.get(m.group(1) or m.group(2), m.group(0)), statement)
        expanded.append((number, statement))
        assigned = ASSIGNMENT.match(statement)
        if assigned:
            value = literal_value(assigned.group(2))
            if value is None:
                known.pop(assigned.group(1), None)
            else:
                known[assigned.group(1)] = value
    return expanded


def shell_statements(lines: Numbered, split_steps: bool) -> Numbered:
    """Return continuation-joined statements with literal variables substituted."""
    return expand_literals(join_continuations(lines), split_steps)


def job_image_hits(unit: Unit) -> list[str]:
    """Return job ``services:`` and ``container:`` images that name NornicDB.

    GitHub starts those containers for the job, so an image there is a start
    with no ``run:`` line. A ``container: <image>`` string and an ``image:`` key
    anywhere under a ``services:`` or ``container:`` block both count.
    """
    hits = []
    block_indent: int | None = None
    for number, line in unit.lines:
        indent = len(line) - len(line.lstrip())
        if block_indent is not None and indent <= block_indent:
            block_indent = None
        block = JOB_CONTAINER.match(line)
        image = JOB_IMAGE.match(line) if block_indent is not None else None
        value = None
        if block:
            value = block.group(2)
            block_indent = indent if not value.strip() else None
        elif image:
            value = image.group(1)
        if value and re.search(r"nornicdb", value, re.IGNORECASE):
            hits.append(f"{unit.subject} runs a NornicDB image as a job service or container "
                        f"({unit.label}:{number}: {line.strip()})")
    return hits


def unit_marker_hits(unit: Unit) -> list[str]:
    """Return one message per workflow-style start marker found in a unit's statements."""
    hits = job_image_hits(unit)
    for number, statement in shell_statements(unit.lines, split_steps=True):
        for pattern, reason in START_MARKERS:
            if reason in RUN_ONLY_REASONS and NAME_LINE.match(statement):
                continue
            if pattern.search(statement):
                hits.append(f"{unit.subject} {reason} ({unit.label}:{number}: {statement.strip()})")
                break
    return hits


def python_starts(text: str) -> list[str]:
    """Return the process-spawning calls of a Python script that start NornicDB.

    The script is parsed, not scanned as text: a string that only mentions a
    command (a fixture, a regex, a docstring) is not a call and is skipped.
    A call counts when the string constants in its arguments, joined with
    spaces, read as ``docker run ... nornicdb`` or a compose ``up ... nornicdb``.
    An argument that is a variable is not resolved; the shell and workflow
    checks still see the common cases.
    """
    try:
        tree = ast.parse(text)
    except SyntaxError as err:
        return [f"cannot be parsed ({err.msg}), so its docker use is unknown"]
    starts = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        func = node.func
        name = func.attr if isinstance(func, ast.Attribute) else getattr(func, "id", "")
        if name not in PY_SPAWNERS:
            continue
        words = [n.value for arg in node.args for n in ast.walk(arg)
                 if isinstance(n, ast.Constant) and isinstance(n.value, str)]
        command = " ".join(words)
        if PY_COMMAND_START.search(command):
            starts.append(f"starts NornicDB from Python (line {node.lineno}: {command[:80]})")
    return starts


def script_starts(text: str, python: bool = False) -> list[str]:
    """Return the statements of a script that start NornicDB.

    A shell script is read statement by statement after backslash-newline
    continuations are joined and literal variables are substituted. Comments
    are skipped, and only a statement that is itself the command counts (see
    ``SCRIPT_START_MARKERS``).
    """
    if python:
        return python_starts(text)
    starts = []
    for number, line in shell_statements(list(enumerate(text.splitlines(), 1)), False):
        if line.strip().startswith("#"):
            continue
        for command in shell.commands(line):
            if SCRIPT_DOCKER_RUN.match(command):
                starts.append(f"docker-runs a NornicDB image (line {number}: {command[:80]})")
                continue
            for pattern, reason in SCRIPT_START_MARKERS:
                if pattern.search(command):
                    starts.append(f"{reason} (line {number}: {command[:80]})")
                    break
    return starts


def command_words(text: str) -> list[str]:
    """Return the script paths a shell or workflow text runs as commands.

    Each line is split into the commands it runs (see ``nornicdb_ci_off_shell``),
    so ``cd x && ./a.sh``, ``sudo ./a.sh``, ``timeout 60 ./a.sh`` and
    ``bash -c './a.sh'`` all reach ``a.sh``.
    """
    words = []
    for _, line in join_continuations(list(enumerate(text.splitlines(), 1))):
        if line.strip().startswith("#"):
            continue
        for command in shell.commands(re.sub(r"^\s*(?:-\s+)?(?:run:\s*)?", "", line)):
            match = RUNS_SCRIPT.match(command)
            if match:
                words.append(match.group(1))
    return words


def python_command_words(text: str) -> list[str]:
    """Return the script paths a Python script passes to a process-spawning call."""
    try:
        tree = ast.parse(text)
    except SyntaxError:
        return []
    words = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Call):
            func = node.func
            name = func.attr if isinstance(func, ast.Attribute) else getattr(func, "id", "")
            if name in PY_SPAWNERS:
                words += [n.value for arg in node.args for n in ast.walk(arg)
                          if isinstance(n, ast.Constant) and isinstance(n.value, str)
                          and re.fullmatch(r"[A-Za-z0-9_./-]+\.(?:sh|py)", n.value)]
    return words


def script_refs(root: Path, words: list[str], base: Path | None) -> list[str]:
    """Resolve script words to repo-relative paths of files that exist in the tree.

    A word resolves against the repository root, the ``scripts`` directory, and
    (for a reference inside a script or an action) that file's own directory.
    A leading ``${VAR}/`` or ``${{ expr }}/`` is dropped first.
    """
    found: list[str] = []
    for word in words:
        token = re.sub(r"^(?:\$\{\{[^}]*\}\}|\$\{?\w+\}?)/", "", word)
        candidates = [root / token, root / "scripts" / token]
        if base is not None:
            candidates.append(base / token)
        for candidate in candidates:
            if candidate.is_file():
                rel = Path(os.path.normpath(candidate)).relative_to(root).as_posix()
                if rel not in found:
                    found.append(rel)
                break
    return found


def script_violations(root: Path, unit: Unit) -> list[str]:
    """Return starts found in every script a unit runs, however deep the chain.

    The walk keeps a ``seen`` set, so a cycle ends it and each script is read once.
    """
    errors: list[str] = []
    seen: set[str] = set()
    text = "\n".join(line for _, line in unit.lines)
    queue: list[tuple[str, str | None]] = [
        (ref, None) for ref in script_refs(root, command_words(text), unit.base)]
    while queue:
        ref, parent = queue.pop(0)
        if ref in seen:
            continue
        seen.add(ref)
        body = read_text(root / ref)
        via = f" (run by {parent})" if parent else ""
        for start in script_starts(body, python=ref.endswith(".py")):
            errors.append(f"{unit.subject} runs {ref}{via}, which {start}")
        inner_words = python_command_words(body) if ref.endswith(".py") else command_words(body)
        queue.extend((inner, ref) for inner in script_refs(root, inner_words, (root / ref).parent))
    return errors


def job_units(root: Path, label: str, job_lines: Numbered) -> list[Unit]:
    """Return a live job as a unit plus every local composite action it reaches.

    An action is read once per job, so actions that call each other end the walk.
    """
    units = [Unit(label, job_lines, None, "a live job")]
    seen: set[str] = set()
    pending = list(job_lines)
    while pending:
        _, line = pending.pop(0)
        match = LOCAL_ACTION.match(line)
        if not match or match.group(1) in seen:
            continue
        seen.add(match.group(1))
        action_dir = Path(os.path.normpath(root / ".github/actions" / match.group(1)))
        for name in ("action.yml", "action.yaml"):
            path = action_dir / name
            if path.is_file() and action_dir.is_relative_to(root):
                lines = numbered_active(read_text(path))
                rel = path.relative_to(root).as_posix()
                units.append(Unit(rel, lines, action_dir,
                                  f"a live job (through composite action {rel})"))
                pending.extend(lines)
                break
    return units


def unit_violations(root: Path, workflow: str, job_id: str, unit: Unit) -> list[str]:
    """Return every start a unit makes directly or through the scripts it runs."""
    return [f"{workflow}/{job_id}: {message}; {OFF_NOTE}"
            for message in unit_marker_hits(unit) + script_violations(root, unit)]
