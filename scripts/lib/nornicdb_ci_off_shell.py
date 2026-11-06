"""Shell statement reading for the NornicDB CI-off guard.

A shell line can hold several commands, and a command can sit behind a wrapper:
``cd x && docker ...``, ``if ...; then docker ...``, ``sudo docker ...``,
``timeout 60 docker ...``, ``bash -c 'docker ...'``. The guard's start markers
match a statement only when the statement itself is the command, so a line that
merely quotes a command (a test assertion, a regex, a usage string) stays
quiet. This module turns one line into the commands it really runs, so the
markers see ``docker ...`` whichever form carried it.

Separators count only outside quotes, so ``echo "a && docker run nornicdb"``
yields one ``echo`` statement and no ``docker`` command. A ``-c`` body or an
``eval`` string is read as shell in turn.

Not resolved: a command inside ``$(...)`` or backticks that is itself inside
double quotes, a wrapper with an option this module does not know, and
``xargs`` or ``find -exec``. Those stay stated limits of the guard.
"""

from __future__ import annotations

import re

_SEPARATOR_CHARS = {";", "|", "(", ")"}
_LEADING_KEYWORD = re.compile(
    r"^(?:then|do|else|if|elif|while|until|!|exec|nohup|command|builtin)\s+")
_LEADING_WRAPPERS = (
    re.compile(r"^sudo(?:\s+(?:-[ughpCDRTU]\s+\S+|-\S+))*\s+"),
    re.compile(r"^timeout(?:\s+(?:-[sk]\s+\S+|-\S+))*\s+\d+(?:\.\d+)?[smhd]?\s+"),
    re.compile(r"^env(?:\s+(?:-\S+|\w+=\S*))*\s+"),
    re.compile(r"^nice(?:\s+(?:-n\s+\S+|-\S+))*\s+"),
    re.compile(r"^time(?:\s+-\S+)*\s+"),
)
_SHELL_STRING = re.compile(
    r"^(?:(?:bash|sh|zsh|dash|ksh)\b(?:\s+-[A-Za-z]+)*?\s+-[A-Za-z]*c|eval)\s+"
    r"(?P<q>[\"'])(?P<body>(?:(?!(?P=q)).)*)(?P=q)")
_MAX_NESTING = 4


def split_statements(line: str) -> list[str]:
    """Split a shell line at ``&&``, ``||``, ``;``, ``|``, ``(`` and ``)`` outside quotes."""
    statements: list[str] = []
    current: list[str] = []
    quote = ""
    index = 0
    while index < len(line):
        char = line[index]
        if quote:
            current.append(char)
            if char == "\\" and quote == '"' and index + 1 < len(line):
                index += 1
                current.append(line[index])
            elif char == quote:
                quote = ""
        elif char == "\\" and index + 1 < len(line):
            current.append(char + line[index + 1])
            index += 1
        elif char in ("'", '"'):
            quote = char
            current.append(char)
        elif char == "&" and line[index:index + 2] == "&&":
            statements.append("".join(current))
            current = []
            index += 1
        elif char in _SEPARATOR_CHARS:
            statements.append("".join(current))
            current = []
            if char == "|" and line[index:index + 2] == "||":
                index += 1
        else:
            current.append(char)
        index += 1
    statements.append("".join(current))
    return [statement.strip() for statement in statements if statement.strip()]


def strip_wrappers(statement: str) -> str:
    """Drop leading keywords and wrappers (``then``, ``sudo``, ``timeout N``, ``env A=b``)."""
    while True:
        before = statement
        statement = _LEADING_KEYWORD.sub("", statement, count=1)
        for wrapper in _LEADING_WRAPPERS:
            statement = wrapper.sub("", statement, count=1)
        if statement == before:
            return statement.strip()


def commands(line: str, depth: int = 0) -> list[str]:
    """Return the commands a shell line runs, wrappers removed.

    A ``bash -c '...'``, ``sh -c "..."`` or ``eval '...'`` body is read as shell
    too, to ``_MAX_NESTING`` levels; the outer statement stays in the result.
    """
    found: list[str] = []
    for statement in split_statements(line):
        command = strip_wrappers(statement)
        if not command:
            continue
        found.append(command)
        inner = _SHELL_STRING.match(command)
        if inner and depth < _MAX_NESTING:
            found.extend(commands(inner.group("body"), depth + 1))
    return found
