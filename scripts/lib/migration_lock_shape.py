#!/usr/bin/env python3
"""Refuses a migration that holds a table lock through bulk DML (#7961).

The Eshu migrator sends each migration file as one simple-protocol message.
PostgreSQL runs a multi-statement message as one implicit transaction, so a
lock taken by an early statement is held until the last statement commits.
Migration 169 combined ``ALTER TABLE content_files`` with a backfill
``UPDATE`` and blocked every content read for the whole backfill (96.3 s on
the #7961 fixture) until #7946 taught the migrator to phase that one file.

This check fails a migration file that combines table-locking DDL on a table
that already exists with bulk DML in the same file. Split such a file: the
DDL in one migration, the DML in the next. Table-locking DDL is ALTER TABLE,
non-CONCURRENT CREATE INDEX, DROP INDEX, REINDEX and REFRESH MATERIALIZED
VIEW, CLUSTER, VACUUM FULL, LOCK, and TRUNCATE. Bulk DML is UPDATE, DELETE,
MERGE, and INSERT ... SELECT, including inside a DO block. DDL on a table
created in the same file is exempt: nobody else can hold or wait for a lock
on a table that did not exist before the transaction. Function bodies do
not run at migration time and are not scanned. There is no allowlist.

Usage: migration_lock_shape.py check NAME < migration.sql
Exit codes: 0 clean, 1 violation (described on stderr), 2 usage error.
"""

import re
import sys

_NAME = r'(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(?:ONLY\s+)?("?[\w.]+"?)'

# Each entry: label, statement pattern, target-table pattern (or None when the
# statement does not name a table, so it can never be exempt).
_LOCKING_DDL = (
    ('ALTER TABLE', re.compile(r'^ALTER\s+TABLE\b', re.I),
     re.compile(r'^ALTER\s+TABLE\s+' + _NAME, re.I)),
    ('CREATE INDEX without CONCURRENTLY',
     re.compile(r'^CREATE\s+(?:UNIQUE\s+)?INDEX\b(?!\s+CONCURRENTLY)', re.I),
     re.compile(r'^CREATE\s+(?:UNIQUE\s+)?INDEX\b.*?\bON\s+(?:ONLY\s+)?("?[\w.]+"?)', re.I | re.S)),
    ('DROP INDEX without CONCURRENTLY',
     re.compile(r'^DROP\s+INDEX\b(?!\s+CONCURRENTLY)', re.I), None),
    ('REINDEX without CONCURRENTLY',
     re.compile(r'^REINDEX\b(?!.*\bCONCURRENTLY\b)', re.I | re.S), None),
    ('REFRESH MATERIALIZED VIEW without CONCURRENTLY',
     re.compile(r'^REFRESH\s+MATERIALIZED\s+VIEW\b(?!\s+CONCURRENTLY)', re.I), None),
    ('CLUSTER', re.compile(r'^CLUSTER\b', re.I),
     re.compile(r'^CLUSTER\s+(?:VERBOSE\s+)?("?[\w.]+"?)', re.I)),
    ('VACUUM FULL', re.compile(r'^VACUUM\s*(?:\([^)]*\bFULL\b[^)]*\)|FULL\b)', re.I),
     re.compile(r'^VACUUM\s*(?:\([^)]*\))?\s*(?:FULL\s+)?("?[\w.]+"?)', re.I)),
    ('LOCK', re.compile(r'^LOCK\b', re.I),
     re.compile(r'^LOCK\s+(?:TABLE\s+)?(?:ONLY\s+)?("?[\w.]+"?)', re.I)),
    ('TRUNCATE', re.compile(r'^TRUNCATE\b', re.I),
     re.compile(r'^TRUNCATE\s+(?:TABLE\s+)?(?:ONLY\s+)?("?[\w.]+"?)', re.I)),
)

_WITH_PREFIX = r'^(?:WITH\b.*?\)\s*)?'
_BULK_DML = (
    ('UPDATE', re.compile(_WITH_PREFIX + r'UPDATE\b', re.I | re.S)),
    ('DELETE', re.compile(_WITH_PREFIX + r'DELETE\b', re.I | re.S)),
    ('MERGE', re.compile(_WITH_PREFIX + r'MERGE\b', re.I | re.S)),
    ('INSERT ... SELECT', re.compile(_WITH_PREFIX + r'INSERT\s+INTO\b.*\bSELECT\b', re.I | re.S)),
)

_CREATE_TABLE = re.compile(
    r'^CREATE\s+(?:UNLOGGED\s+|TEMP(?:ORARY)?\s+)?TABLE\s+' + _NAME, re.I)
_DOLLAR_TAG = re.compile(r'\$(?:[A-Za-z_][A-Za-z0-9_]*)?\$')


def split_statements(sql: str) -> list[str]:
    """Splits SQL into statements with comments and literals blanked.

    Comments become whitespace and string literals become ''. A dollar-quoted
    body is dropped, except the body of a DO block, whose statements run at
    migration time and are returned as statements of their own.

    Args:
        sql: The migration file content.

    Returns:
        The non-empty statements, stripped, in file order.
    """
    statements: list[str] = []
    current: list[str] = []
    i, n = 0, len(sql)
    while i < n:
        if sql.startswith('--', i):
            end = sql.find('\n', i)
            i = n if end < 0 else end
            continue
        if sql.startswith('/*', i):
            end = sql.find('*/', i + 2)
            i = n if end < 0 else end + 2
            current.append(' ')
            continue
        if sql[i] == "'":
            j = i + 1
            while j < n:
                if sql.startswith("''", j):
                    j += 2
                    continue
                if sql[j] == "'":
                    break
                j += 1
            current.append("''")
            i = j + 1
            continue
        tag = _DOLLAR_TAG.match(sql, i)
        if tag:
            close = sql.find(tag.group(0), tag.end())
            body = sql[tag.end():] if close < 0 else sql[tag.end():close]
            i = n if close < 0 else close + len(tag.group(0))
            if re.match(r'^\s*DO\b', ''.join(current), re.I):
                statements.extend(split_statements(_strip_plpgsql_block(body)))
            current.append(' $body$ ')
            continue
        if sql[i] == ';':
            statements.append(''.join(current).strip())
            current = []
        else:
            current.append(sql[i])
        i += 1
    statements.append(''.join(current).strip())
    return [s for s in statements if s]


def _strip_plpgsql_block(body: str) -> str:
    """Removes BEGIN/END and DECLARE framing so inner statements lead."""
    body = re.sub(r'(?is)^\s*DECLARE\b.*?\bBEGIN\b', ';', body)
    return re.sub(r'(?i)\b(?:BEGIN|END(?:\s+IF|\s+LOOP)?|IF\b[^;]*?\bTHEN|ELSE|LOOP)\b', ';', body)


def _table(name: str) -> str:
    return name.strip('"').lower().split('.')[-1]


def find_violation(sql: str) -> str | None:
    """Returns a description of a lock-through-DML violation, or None.

    Args:
        sql: The migration file content.

    Returns:
        A one-line description naming the locking statement kinds and the
        bulk DML kinds, or None when the file is clean.
    """
    statements = split_statements(sql)
    created = {_table(m.group(1)) for s in statements if (m := _CREATE_TABLE.search(s))}
    locking: list[str] = []
    for statement in statements:
        for label, pattern, target in _LOCKING_DDL:
            if not pattern.search(statement):
                continue
            match = target.search(statement) if target else None
            if match and _table(match.group(1)) in created:
                continue
            locking.append(label)
    bulk = [label for s in statements for label, pattern in _BULK_DML if pattern.search(s)]
    if not locking or not bulk:
        return None
    return (f"table-locking DDL ({', '.join(sorted(set(locking)))}) and bulk DML "
            f"({', '.join(sorted(set(bulk)))}) in one file hold the lock through the DML")


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
