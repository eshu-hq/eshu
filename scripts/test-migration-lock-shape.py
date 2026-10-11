#!/usr/bin/env python3
"""Seeded RED/GREEN tests for scripts/lib/migration_lock_shape.py (#7961)."""

import pathlib
import subprocess
import sys
import unittest

ROOT = pathlib.Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / 'scripts' / 'lib'))

import migration_lock_shape  # noqa: E402  pylint: disable=wrong-import-position

MIGRATIONS = ROOT / 'go' / 'internal' / 'storage' / 'postgres' / 'migrations'


class FindViolationTest(unittest.TestCase):
    """Covers the violation shapes and the exemptions."""

    def test_shipped_169_shape_is_refused(self) -> None:
        """RED: the exact shipped 169 file, if added today, fails."""
        sql = (MIGRATIONS / '169_content_files_generation_id.sql').read_text(encoding='utf-8')
        violation = migration_lock_shape.find_violation(sql)
        self.assertIsNotNone(violation)
        self.assertIn('ALTER TABLE', violation)
        self.assertIn('UPDATE', violation)

    def test_locking_ddl_with_bulk_dml_is_refused(self) -> None:
        """RED: each locking DDL kind combined with each bulk DML kind fails."""
        ddl = (
            'ALTER TABLE t ADD COLUMN c TEXT',
            'CREATE INDEX t_c_idx ON t (c)',
            'CREATE UNIQUE INDEX IF NOT EXISTS t_c_idx ON ONLY t (c)',
            'DROP INDEX t_c_idx',
            'REINDEX TABLE t',
            'REFRESH MATERIALIZED VIEW v',
            'CLUSTER t USING t_pkey',
            'VACUUM FULL t',
            'VACUUM (FULL, ANALYZE) t',
            'LOCK TABLE t IN ACCESS EXCLUSIVE MODE',
            'TRUNCATE t',
        )
        dml = (
            'UPDATE t SET c = 1',
            'DELETE FROM t WHERE c IS NULL',
            'INSERT INTO u (c) SELECT c FROM t',
            'WITH x AS (SELECT 1) UPDATE t SET c = 1',
            'MERGE INTO t USING u ON t.id = u.id WHEN MATCHED THEN DELETE',
        )
        for statement in ddl:
            for change in dml:
                with self.subTest(ddl=statement, dml=change):
                    self.assertIsNotNone(
                        migration_lock_shape.find_violation(f'{statement};\n{change};\n'))

    def test_bulk_dml_inside_do_block_is_refused(self) -> None:
        """RED: a DO block runs at migration time, so its UPDATE counts."""
        sql = ('ALTER TABLE t ADD COLUMN c TEXT;\n'
               'DO $$\nBEGIN\n  IF true THEN\n    UPDATE t SET c = 1;\n  END IF;\nEND $$;\n')
        self.assertIsNotNone(migration_lock_shape.find_violation(sql))

    def test_safe_shapes_pass(self) -> None:
        """GREEN: single-purpose and non-blocking files pass."""
        clean = (
            'ALTER TABLE t ADD COLUMN IF NOT EXISTS c TEXT NULL;',
            'UPDATE t SET c = 1 WHERE c IS NULL;\nANALYZE t;',
            'CREATE INDEX CONCURRENTLY IF NOT EXISTS t_c_idx ON t (c);',
            'DROP INDEX CONCURRENTLY IF EXISTS t_c_idx;\nUPDATE t SET c = 1;',
            'INSERT INTO t (c) VALUES (1);\nALTER TABLE t SET (fillfactor = 90);',
            # A new table cannot block anyone, so DDL on it may sit with DML.
            'CREATE TABLE IF NOT EXISTS u (c TEXT);\nCREATE INDEX u_c_idx ON u (c);\n'
            'ALTER TABLE u ADD COLUMN d TEXT;\nINSERT INTO u (c) SELECT c FROM t;',
            # A function body does not run at migration time.
            'ALTER TABLE t ADD COLUMN c TEXT;\nCREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$\n'
            'BEGIN UPDATE t SET c = 1; RETURN NEW; END $$;',
            # Keywords inside comments and string literals are not statements.
            "-- UPDATE t SET c = 1;\nALTER TABLE t ADD COLUMN c TEXT DEFAULT 'UPDATE t';\n/* DELETE FROM t; */",
        )
        for sql in clean:
            with self.subTest(sql=sql):
                self.assertIsNone(migration_lock_shape.find_violation(sql))


class CommandLineTest(unittest.TestCase):
    """Covers the CLI contract the immutability gate calls."""

    def run_cli(self, sql: str, *args: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, str(ROOT / 'scripts' / 'lib' / 'migration_lock_shape.py'), *args],
            input=sql, capture_output=True, text=True, check=False)

    def test_violation_exits_one_and_names_the_file(self) -> None:
        result = self.run_cli('ALTER TABLE t ADD COLUMN c TEXT;\nUPDATE t SET c = 1;', 'check', '170_x.sql')
        self.assertEqual(result.returncode, 1)
        self.assertIn('170_x.sql', result.stderr)
        self.assertIn('7961', result.stderr)

    def test_clean_exits_zero(self) -> None:
        result = self.run_cli('ALTER TABLE t ADD COLUMN c TEXT;', 'check', '170_x.sql')
        self.assertEqual(result.returncode, 0)

    def test_usage_error_exits_two(self) -> None:
        self.assertEqual(self.run_cli('', 'check').returncode, 2)


if __name__ == '__main__':
    unittest.main()
