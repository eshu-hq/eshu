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

    def test_review_f2_shapes_are_refused(self) -> None:
        """RED: every false negative from #7961 review F2 now fails."""
        alter = 'ALTER TABLE t ADD COLUMN c int;\n'
        cases = {
            'ELSIF branch': alter + 'DO $$ BEGIN IF false THEN NULL; ELSIF true THEN UPDATE t SET c = 1; END IF; END $$;',
            'EXCEPTION handler': alter + 'DO $$ BEGIN PERFORM 1; EXCEPTION WHEN others THEN UPDATE t SET c = 1; END $$;',
            'CASE WHEN branch': alter + 'DO $$ BEGIN CASE WHEN true THEN DELETE FROM t; END CASE; END $$;',
            'EXECUTE literal': alter + "DO $$ BEGIN EXECUTE 'UPDATE t SET c = 1'; END $$;",
            'EXECUTE format': alter + "DO $$ BEGIN EXECUTE format('DELETE FROM %I', 't'); END $$;",
            'data-modifying CTE under SELECT': alter + 'WITH u AS (UPDATE t SET c = 1 RETURNING 1) SELECT count(*) FROM u;',
            'function created and called': alter + (
                'CREATE OR REPLACE FUNCTION bf() RETURNS void LANGUAGE plpgsql AS $fn$\n'
                'BEGIN UPDATE t SET c = 1; END $fn$;\nSELECT bf();'),
            'procedure created and called': alter + (
                'CREATE PROCEDURE bp() LANGUAGE sql AS $$ DELETE FROM t $$;\nCALL bp();'),
            'function performed in DO': alter + (
                'CREATE FUNCTION bf() RETURNS void LANGUAGE sql AS $$ UPDATE t SET c = 1 $$;\n'
                'DO $$ BEGIN PERFORM bf(); END $$;'),
            'E-string escape': "ALTER TABLE t ADD COLUMN c TEXT DEFAULT E'it\\'s';\nUPDATE t SET c = 'x';",
            'CREATE TABLE IF NOT EXISTS on an existing name': (
                'CREATE TABLE IF NOT EXISTS t (id int);\n' + alter + 'UPDATE t SET c = 1;'),
            'dynamic DDL': "DO $$ BEGIN EXECUTE 'ALTER TABLE t ADD COLUMN c int'; END $$;\nUPDATE t SET c = 1;",
            'DROP TRIGGER': 'DROP TRIGGER IF EXISTS tr ON t;\nUPDATE t SET c = 1;',
            'CREATE TRIGGER': 'CREATE TRIGGER tr AFTER INSERT ON t FOR EACH ROW EXECUTE FUNCTION f();\nUPDATE t SET c = 1;',
            'DROP TABLE': 'DROP TABLE IF EXISTS u;\nUPDATE t SET c = 1;',
            'ALTER INDEX': 'ALTER INDEX t_c_idx RENAME TO t_c_idx_old;\nUPDATE t SET c = 1;',
            'ALTER MATERIALIZED VIEW': 'ALTER MATERIALIZED VIEW v RENAME TO w;\nUPDATE t SET c = 1;',
        }
        for name, sql in cases.items():
            with self.subTest(shape=name):
                self.assertIsNotNone(migration_lock_shape.find_violation(sql), sql)

    def test_review_f2_guards_stay_clean(self) -> None:
        """GREEN: the F2 fixes do not flag these safe shapes."""
        clean = (
            # A trigger on a table this file creates cannot block anyone.
            'CREATE TABLE u (c int);\nCREATE TRIGGER tr AFTER INSERT ON u FOR EACH ROW EXECUTE FUNCTION f();\n'
            'INSERT INTO u (c) SELECT c FROM t;',
            # A function defined but never called does not run.
            'ALTER TABLE t ADD COLUMN c int;\nCREATE FUNCTION bf() RETURNS void LANGUAGE sql AS $$ UPDATE t SET c = 1 $$;',
            # A quoted identifier may hold a semicolon or a quote.
            'ALTER TABLE "we;i\'rd" ADD COLUMN c int;',
            # An escape string that ends cleanly does not swallow later text.
            "UPDATE t SET c = E'a\\\\';\nANALYZE t;",
            # A DO block with only reads and no locking DDL.
            "DO $$ BEGIN IF EXISTS (SELECT 1 FROM t) THEN RAISE NOTICE 'x'; END IF; END $$;",
        )
        for sql in clean:
            with self.subTest(sql=sql):
                self.assertIsNone(migration_lock_shape.find_violation(sql))

    def test_safe_shapes_pass(self) -> None:
        """GREEN: single-purpose and non-blocking files pass."""
        clean = (
            'ALTER TABLE t ADD COLUMN IF NOT EXISTS c TEXT NULL;',
            'UPDATE t SET c = 1 WHERE c IS NULL;\nANALYZE t;',
            'CREATE INDEX CONCURRENTLY IF NOT EXISTS t_c_idx ON t (c);',
            'DROP INDEX CONCURRENTLY IF EXISTS t_c_idx;\nUPDATE t SET c = 1;',
            'INSERT INTO t (c) VALUES (1);\nALTER TABLE t SET (fillfactor = 90);',
            # A new table cannot block anyone, so DDL on it may sit with DML.
            'CREATE TABLE u (c TEXT);\nCREATE INDEX u_c_idx ON u (c);\n'
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
