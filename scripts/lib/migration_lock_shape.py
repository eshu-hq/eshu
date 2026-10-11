#!/usr/bin/env python3
"""Refuses a migration that holds a table lock through bulk DML (#7961).

The Eshu migrator sends each migration file as one simple-protocol message.
PostgreSQL runs a multi-statement message as one implicit transaction, so a
lock taken by an early statement is held until the last statement commits.
Migration 169 combined ``ALTER TABLE content_files`` with a backfill
``UPDATE`` and blocked every content read for the whole backfill (96.3 s on
the #7961 fixture) until #7946 taught the migrator to phase that one file.

This check fails a migration file that combines table-locking DDL on a table
that may already exist with bulk DML in the same file. Split such a file: the
DDL in one migration, the DML in the next.

Table-locking DDL: ALTER TABLE, ALTER INDEX, ALTER MATERIALIZED VIEW,
CREATE INDEX or DROP INDEX without CONCURRENTLY, REINDEX or REFRESH
MATERIALIZED VIEW without CONCURRENTLY, CREATE TRIGGER, DROP TRIGGER,
CREATE RULE, DROP RULE, DROP TABLE, CLUSTER, VACUUM FULL, TRUNCATE, and LOCK
in any mode except ACCESS SHARE, ROW SHARE, or ROW EXCLUSIVE. Those three
conflict with neither readers (ACCESS SHARE) nor writers (ROW EXCLUSIVE) in
PostgreSQL's table-lock conflict table, and none is stronger than the ROW
EXCLUSIVE the backfill's own UPDATE takes. A bare LOCK is ACCESS EXCLUSIVE.

Bulk DML: UPDATE, DELETE, MERGE, INSERT ... SELECT, and a data-modifying CTE.

Statements that run at migration time are checked: top-level statements, the
body of a DO block (including IF/ELSIF/ELSE, CASE WHEN, LOOP and EXCEPTION
WHEN branches), and the body of a function or procedure created in the same
file when an executed statement names it (for example ``SELECT f()``,
``PERFORM f()``, or ``CALL p()``). In those bodies, an EXECUTE counts as DDL
or DML when any single-quoted or dollar-quoted text of the body names a DDL or
DML keyword: the check cannot evaluate the string, so it assumes the worst.
A RAISE message that names UPDATE beside an EXECUTE of DDL is therefore
refused too.

Exemption: DDL on a table that a plain ``CREATE TABLE`` in the same file
creates, because no other session can hold or wait for a lock on it. ``CREATE
TABLE IF NOT EXISTS`` does not exempt: the table may already exist. Do not
write a plain ``CREATE TABLE`` to earn the exemption: every migration must
stay re-runnable, because the local supervisor re-applies every definition
on each start with no ledger, and a plain ``CREATE TABLE`` fails the second
time. For a new table, use the split form: the table and its indexes with IF
NOT EXISTS in migration N, the idempotent seed or backfill in migration N+1.
There is no allowlist.

Conservative choices: statement order is ignored, so DDL after the last DML
in a file is refused too. LOCK IN SHARE UPDATE EXCLUSIVE MODE is refused
although it blocks neither reads nor writes, because it blocks VACUUM,
ANALYZE, and index builds for the whole backfill.

Static limits (the check does not see these): a trigger function fired by
the file's own DML; a function or procedure defined in another migration; a
SQL-language function body written as a single-quoted string; EXECUTE of
text that no literal in the body contains; a foreign key that exempt DDL on
a new table adds, which locks the referenced table (SHARE ROW EXCLUSIVE).

Usage: migration_lock_shape.py check NAME < migration.sql
Exit codes: 0 clean, 1 violation (described on stderr), 2 usage error.
"""

import re
import sys
from dataclasses import dataclass, field

_IDENT = r'("?[\w.]+"?)'
_NAME = r'(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(?:ONLY\s+)?' + _IDENT
_ON_TABLE = re.compile(r'\bON\s+(?:ONLY\s+)?' + _IDENT, re.I)

# Each entry: label, statement pattern, target-table pattern (or None when the
# statement names no table, so it can never be exempt).
_LOCKING_DDL = (
    ('ALTER TABLE', re.compile(r'^ALTER\s+TABLE\b', re.I),
     re.compile(r'^ALTER\s+TABLE\s+' + _NAME, re.I)),
    ('ALTER INDEX', re.compile(r'^ALTER\s+INDEX\b', re.I), None),
    ('ALTER MATERIALIZED VIEW', re.compile(r'^ALTER\s+MATERIALIZED\s+VIEW\b', re.I), None),
    ('CREATE INDEX without CONCURRENTLY',
     re.compile(r'^CREATE\s+(?:UNIQUE\s+)?INDEX\b(?!\s+CONCURRENTLY)', re.I), _ON_TABLE),
    ('DROP INDEX without CONCURRENTLY',
     re.compile(r'^DROP\s+INDEX\b(?!\s+CONCURRENTLY)', re.I), None),
    ('REINDEX without CONCURRENTLY',
     re.compile(r'^REINDEX\b(?!.*\bCONCURRENTLY\b)', re.I | re.S), None),
    ('REFRESH MATERIALIZED VIEW without CONCURRENTLY',
     re.compile(r'^REFRESH\s+MATERIALIZED\s+VIEW\b(?!\s+CONCURRENTLY)', re.I), None),
    ('CREATE TRIGGER',
     re.compile(r'^CREATE\s+(?:OR\s+REPLACE\s+)?(?:CONSTRAINT\s+)?TRIGGER\b', re.I), _ON_TABLE),
    ('DROP TRIGGER', re.compile(r'^DROP\s+TRIGGER\b', re.I), _ON_TABLE),
    ('CREATE RULE', re.compile(r'^CREATE\s+(?:OR\s+REPLACE\s+)?RULE\b', re.I), _ON_TABLE),
    ('DROP RULE', re.compile(r'^DROP\s+RULE\b', re.I), _ON_TABLE),
    ('DROP TABLE', re.compile(r'^DROP\s+TABLE\b', re.I),
     re.compile(r'^DROP\s+TABLE\s+' + _NAME, re.I)),
    ('CLUSTER', re.compile(r'^CLUSTER\b', re.I),
     re.compile(r'^CLUSTER\s+(?:VERBOSE\s+)?' + _IDENT, re.I)),
    ('VACUUM FULL', re.compile(r'^VACUUM\s*(?:\([^)]*\bFULL\b[^)]*\)|FULL\b)', re.I),
     re.compile(r'^VACUUM\s*(?:\([^)]*\))?\s*(?:FULL\s+)?' + _IDENT, re.I)),
    # ACCESS SHARE, ROW SHARE and ROW EXCLUSIVE conflict with neither readers
    # nor writers and are no stronger than the backfill's own UPDATE: exempt.
    ('LOCK', re.compile(
        r'^LOCK\b(?!.*\bIN\s+(?:ACCESS\s+SHARE|ROW\s+SHARE|ROW\s+EXCLUSIVE)\s+MODE\b)',
        re.I | re.S),
     re.compile(r'^LOCK\s+(?:TABLE\s+)?(?:ONLY\s+)?' + _IDENT, re.I)),
    ('TRUNCATE', re.compile(r'^TRUNCATE\b', re.I),
     re.compile(r'^TRUNCATE\s+(?:TABLE\s+)?(?:ONLY\s+)?' + _IDENT, re.I)),
)

_WITH_PREFIX = r'^(?:WITH\b.*?\)\s*)?'
_BULK_DML = (
    ('UPDATE', re.compile(_WITH_PREFIX + r'UPDATE\b', re.I | re.S)),
    ('DELETE', re.compile(_WITH_PREFIX + r'DELETE\b', re.I | re.S)),
    ('MERGE', re.compile(_WITH_PREFIX + r'MERGE\b', re.I | re.S)),
    ('INSERT ... SELECT', re.compile(_WITH_PREFIX + r'INSERT\s+INTO\b.*\bSELECT\b', re.I | re.S)),
    ('data-modifying CTE', re.compile(
        r'^WITH\b.*\(\s*(?:UPDATE|DELETE|MERGE|INSERT\s+INTO)\b', re.I | re.S)),
)

_PLAIN_CREATE_TABLE = re.compile(
    r'^CREATE\s+(?:UNLOGGED\s+|TEMP(?:ORARY)?\s+)?TABLE\s+(?!IF\s+NOT\s+EXISTS\b)' + _IDENT, re.I)
_ROUTINE = re.compile(
    r'^CREATE\s+(?:OR\s+REPLACE\s+)?(?:FUNCTION|PROCEDURE)\s+' + _IDENT, re.I)
_NOT_A_CALL_SITE = re.compile(
    r'^(?:CREATE|COMMENT|GRANT|REVOKE|DROP|ALTER\s+(?:FUNCTION|PROCEDURE|ROUTINE))\b', re.I)
_DO_BLOCK = re.compile(r'^\s*DO\b', re.I)
_PLPGSQL_CONTROL = re.compile(
    r'\b(?:DECLARE|BEGIN|END(?:\s+(?:IF|LOOP|CASE))?|THEN|ELSE|ELSIF|LOOP|EXCEPTION)\b', re.I)
_EXECUTE = re.compile(r'\bEXECUTE\b', re.I)
_DYNAMIC_DML = re.compile(r'\b(?:UPDATE|DELETE|MERGE|COPY|INSERT\s+INTO)\b', re.I)
_DYNAMIC_DDL = re.compile(
    r'\b(?:ALTER\s+(?:TABLE|INDEX)|CREATE\s+(?:UNIQUE\s+)?INDEX|CREATE\s+TRIGGER|DROP|TRUNCATE|'
    r'LOCK|CLUSTER|REINDEX|VACUUM|REFRESH)\b', re.I)
_DOLLAR_TAG = re.compile(r'\$(?:[A-Za-z_][A-Za-z0-9_]*)?\$')


@dataclass
class Statement:
    """One SQL statement after scanning.

    Attributes:
        text: The statement with comments removed, string literals blanked to
            '' and dollar-quoted bodies replaced by a placeholder.
        bodies: The raw dollar-quoted bodies, in order.
        literals: The contents of the string literals, in order.
    """

    text: str
    bodies: list[str] = field(default_factory=list)
    literals: list[str] = field(default_factory=list)


def scan(sql: str) -> list[Statement]:
    """Splits SQL into statements, honoring comments, quotes and dollar bodies.

    Args:
        sql: SQL text: a migration file, or a PL/pgSQL body.

    Returns:
        The non-empty statements in order.
    """
    statements: list[Statement] = []
    current = Statement('')
    text: list[str] = []
    i, n = 0, len(sql)
    while i < n:
        char = sql[i]
        if sql.startswith('--', i):
            end = sql.find('\n', i)
            i = n if end < 0 else end
        elif sql.startswith('/*', i):
            end = sql.find('*/', i + 2)
            i = n if end < 0 else end + 2
            text.append(' ')
        elif char == "'":
            escapes = i > 0 and sql[i - 1] in 'eE' and (i < 2 or not (sql[i - 2].isalnum() or sql[i - 2] == '_'))
            j = i + 1
            while j < n:
                if escapes and sql[j] == '\\':
                    j += 2
                    continue
                if sql.startswith("''", j):
                    j += 2
                    continue
                if sql[j] == "'":
                    break
                j += 1
            current.literals.append(sql[i + 1:j])
            text.append("''")
            i = j + 1
        elif char == '"':
            end = sql.find('"', i + 1)
            end = n if end < 0 else end + 1
            text.append(sql[i:end].replace(';', '_'))
            i = end
        elif tag := _DOLLAR_TAG.match(sql, i):
            close = sql.find(tag.group(0), tag.end())
            current.bodies.append(sql[tag.end():] if close < 0 else sql[tag.end():close])
            text.append(' $body$ ')
            i = n if close < 0 else close + len(tag.group(0))
        elif char == ';':
            current.text = ''.join(text).strip()
            statements.append(current)
            current, text = Statement(''), []
            i += 1
        else:
            text.append(char)
            i += 1
    current.text = ''.join(text).strip()
    statements.append(current)
    return [s for s in statements if s.text]


def _table(name: str) -> str:
    return name.strip('"').lower().split('.')[-1]


@dataclass
class _Findings:
    locking: set[str] = field(default_factory=set)
    bulk: set[str] = field(default_factory=set)


def _check_statement(text: str, created: set[str], findings: _Findings) -> None:
    for label, pattern, target in _LOCKING_DDL:
        if pattern.search(text):
            match = target.search(text) if target else None
            if not (match and _table(match.group(1)) in created):
                findings.locking.add(label)
    for label, pattern in _BULK_DML:
        if pattern.search(text):
            findings.bulk.add(label)


def _run_plpgsql(body: str, routines: dict[str, str], created: set[str],
                 findings: _Findings, visited: set[str]) -> None:
    """Checks a PL/pgSQL body that runs at migration time."""
    statements = scan(body)
    # Dollar-quoted text is EXECUTE input too (shipped 088 uses EXECUTE $ddl$).
    literal_text = ' '.join(text for s in statements for text in (*s.literals, *s.bodies))
    for statement in statements:
        for piece in _PLPGSQL_CONTROL.split(statement.text):
            piece = piece.strip()
            if piece:
                _run(piece, routines, created, findings, visited)
        if _EXECUTE.search(statement.text):
            if _DYNAMIC_DML.search(literal_text):
                findings.bulk.add('EXECUTE of DML text')
            if _DYNAMIC_DDL.search(literal_text):
                findings.locking.add('EXECUTE of DDL text')


def _run(text: str, routines: dict[str, str], created: set[str],
         findings: _Findings, visited: set[str]) -> None:
    """Checks one executed statement and follows calls into same-file routines."""
    _check_statement(text, created, findings)
    if _NOT_A_CALL_SITE.search(text):
        return
    for name, body in routines.items():
        if name not in visited and re.search(r'\b' + re.escape(name) + r'\s*\(', text, re.I):
            visited.add(name)
            _run_plpgsql(body, routines, created, findings, visited)


def find_violation(sql: str) -> str | None:
    """Returns a description of a lock-through-DML violation, or None.

    Args:
        sql: The migration file content.

    Returns:
        A one-line description naming the locking statement kinds and the
        bulk DML kinds, or None when the file is clean.
    """
    statements = scan(sql)
    created = {_table(m.group(1)) for s in statements if (m := _PLAIN_CREATE_TABLE.search(s.text))}
    routines = {_table(m.group(1)): s.bodies[0]
                for s in statements if (m := _ROUTINE.search(s.text)) and s.bodies}
    findings = _Findings()
    visited: set[str] = set()
    for statement in statements:
        if _DO_BLOCK.search(statement.text):
            for body in statement.bodies:
                _run_plpgsql(body, routines, created, findings, visited)
        elif not _ROUTINE.search(statement.text):
            _run(statement.text, routines, created, findings, visited)
    if not findings.locking or not findings.bulk:
        return None
    return (f"table-locking DDL ({', '.join(sorted(findings.locking))}) and bulk DML "
            f"({', '.join(sorted(findings.bulk))}) in one file hold the lock through the DML")


def main(argv: list[str]) -> int:
    """Runs the CLI described in the module docstring."""
    if len(argv) != 3 or argv[1] != 'check':
        print('usage: migration_lock_shape.py check NAME < migration.sql', file=sys.stderr)
        return 2
    violation = find_violation(sys.stdin.read())
    if violation is None:
        return 0
    print(f'{argv[2]}: {violation} -- #7961: split the DDL and the DML into separate migrations',
          file=sys.stderr)
    return 1


if __name__ == '__main__':
    sys.exit(main(sys.argv))
