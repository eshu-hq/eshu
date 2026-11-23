# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Seeded RED/GREEN proof for the fact-set normalisation in assemble_result.py.

GREEN: two runs over identical content that differ only in the run-variant fields
(fact_id, payload.indexed_at, payload.source_run_id) hash equal.
RED:   a changed payload field, a changed stable fact key and a missing fact each
       change the hash, so the normalisation cannot hide a real difference.

Run: python3 -I scripts/dev/10882-replay/test_assemble_result.py
"""

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import assemble_result  # noqa: E402  pylint: disable=wrong-import-position


def _dump(rows: list[tuple[str, str, str, dict]]) -> str:
  handle = tempfile.NamedTemporaryFile("w", suffix=".tsv", delete=False, encoding="utf-8")
  with handle:
    for scope_id, fact_id, key, payload in rows:
      handle.write("%s\t%s\t%s\t%s\n" % (scope_id, fact_id, key, json.dumps(payload)))
  return handle.name


def _rows(run: str, **override) -> list[tuple[str, str, str, dict]]:
  payload = {"relative_path": "src/mod1.go", "language": "go", "indexed_at": "t-" + run, "source_run_id": "run-" + run}
  payload.update(override)
  return [("scope-1", "id-" + run + "-1", "file:src/mod1.go", payload),
          ("scope-1", "id-" + run + "-2", "repository:r", {"name": "r", "source_run_id": "run-" + run})]


class RealFactSetsTest(unittest.TestCase):

  def _hash(self, rows) -> dict:
    path = _dump(rows)
    try:
      return assemble_result.real_fact_sets(path)
    finally:
      os.unlink(path)

  def test_green_run_variant_fields_do_not_change_the_hash(self):
    self.assertEqual(self._hash(_rows("a")), self._hash(_rows("b")))

  def test_red_changed_payload_field_changes_the_hash(self):
    self.assertNotEqual(self._hash(_rows("a")), self._hash(_rows("b", language="rust")))

  def test_red_changed_stable_key_changes_the_hash(self):
    other = _rows("b")
    other[0] = (other[0][0], other[0][1], "file:src/other.go", other[0][3])
    self.assertNotEqual(self._hash(_rows("a")), self._hash(other))

  def test_red_missing_fact_changes_the_hash(self):
    self.assertNotEqual(self._hash(_rows("a")), self._hash(_rows("b")[:1]))


if __name__ == "__main__":
  unittest.main()
