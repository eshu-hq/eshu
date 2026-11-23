# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""RED/GREEN proof of the shape-judged final-state checks on REAL exported rows.

testdata/{C,CF,T1}.tgz hold the export/ files of one real full-timing run of each
arm (6 fixture repositories at the time, filtered to one scope to keep the files
small; commit 97901a5726-era driver, reduced A rows): the control C (active
generation a delta over sha1), the reference CF (one full generation at the final
sha) and the treatment arm T1 (first generation paced and superseded unprojected,
so its later generations are full). GREEN: the real triple passes every judged
final-state check. RED: dropping one row from T1's real exports (an active fact, an
intent, a content row, a relationship count, the sha1 full) or corrupting CF's
repository fact must each fail the matching check.

Run: python3 -I scripts/dev/10882-replay/test_real_rows.py
"""

import os
import shutil
import sys
import tarfile
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import assemble_result  # noqa: E402  pylint: disable=wrong-import-position
import checks_final  # noqa: E402  pylint: disable=wrong-import-position

SOURCES = {"A": "git", "B": "hosted"}
SCOPE = "git-repository-scope:repository:r_28dfe9df"


def _result(arm: str, mutate=None) -> dict:
  """Rebuilds one arm's final state from its real export files, after an optional mutation."""
  tmp = tempfile.mkdtemp()
  try:
    with tarfile.open(os.path.join(HERE, "testdata", arm + ".tgz")) as archive:
      archive.extractall(tmp, filter="data")
    export = os.path.join(tmp, "export")
    if mutate:
      mutate(export)
    return {"final": assemble_result.final_state(export, SOURCES)}
  finally:
    shutil.rmtree(tmp)


def _edit(export: str, name: str, drop=None, replace=None) -> None:
  path = os.path.join(export, name)
  with open(path, encoding="utf-8") as handle:
    lines = handle.read().splitlines(True)
  if drop is not None:
    index = next(i for i, line in enumerate(lines) if drop(line))
    del lines[index]
  if replace is not None:
    lines = [replace(line) for line in lines]
  with open(path, "w", encoding="utf-8") as handle:
    handle.writelines(lines)


def _failed(checks: list) -> list:
  return [c.name for c in checks if not c.passed]


def _active_sha(arm: dict) -> str:
  return arm["final"]["shapes"][SCOPE]["active_sha"]


class RealRowsTest(unittest.TestCase):

  @classmethod
  def setUpClass(cls):
    cls.c, cls.cf, cls.t1 = _result("C"), _result("CF"), _result("T1")

  def test_the_real_shapes_are_the_ones_the_ruling_describes(self):
    self.assertEqual(self.c["final"]["shapes"][SCOPE]["is_delta"], True)
    self.assertEqual(self.cf["final"]["shapes"][SCOPE]["is_delta"], False)
    self.assertEqual(self.t1["final"]["shapes"][SCOPE]["is_delta"], False)
    self.assertEqual(len(self.cf["final"]["shapes"][SCOPE]["sha_chain"]), 1)
    self.assertEqual(self.t1["final"]["shapes"][SCOPE]["sha_chain"], self.c["final"]["shapes"][SCOPE]["sha_chain"])

  def test_green_real_t1_passes_every_judged_final_state_check(self):
    self.assertEqual(_failed(checks_final.check_final_state(self.t1, self.c, self.cf)), [])
    self.assertEqual(_failed(checks_final.check_cf_reference(self.c, self.cf)), [])
    # C against itself: the strict path, no CF needed.
    self.assertEqual(_failed(checks_final.check_final_state(self.c, self.c)), [])

  def test_red_t1_needs_cf_when_its_shape_differs_from_c(self):
    failed = _failed(checks_final.check_final_state(self.t1, self.c, None))
    self.assertIn("every fixture scope matches the shape of C or of CF", failed)

  def test_red_one_active_fact_row_dropped_from_t1(self):
    sha = _active_sha(self.t1)
    mutated = _result("T1", lambda e: _edit(e, "real_facts_chain.tsv", drop=lambda l: l.split("\t")[2] == sha and "\tcontent_entity:" in l))
    self.assertIn("active-generation fact sets equal the reference of the same shape (3-field normalisation)",
                  _failed(checks_final.check_final_state(mutated, self.c, self.cf)))

  def test_red_one_active_intent_dropped_from_t1(self):
    mutated = _result("T1", lambda e: _edit(e, "reducer_rows.tsv", drop=lambda l: l.split("\t")[3] == "t" and l.startswith(SCOPE)))
    failed = _failed(checks_final.check_final_state(mutated, self.c, self.cf))
    self.assertIn("active-generation intents equal C (same shape) or are a superset of C's (full at the final sha)", failed)

  def test_red_one_content_row_dropped_from_t1(self):
    mutated = _result("T1", lambda e: _edit(e, "content_ids.tsv", drop=lambda l: l.startswith("e\t")))
    failed = _failed(checks_final.check_final_state(mutated, self.c, self.cf))
    self.assertIn("content identity sets (files by repo, path and content hash; entities by id)", failed)
    self.assertIn("content rows", failed)

  def test_red_a_swapped_content_entity_keeps_the_count_but_fails_identity(self):
    swapped = []

    def swap(line: str) -> str:
      if line.startswith("e\t") and not swapped:
        swapped.append(line)
        return "e\tcontent-entity:e_swapped\t\t\n"
      return line
    mutated = _result("T1", lambda e: _edit(e, "content_ids.tsv", replace=swap))
    failed = _failed(checks_final.check_final_state(mutated, self.c, self.cf))
    self.assertIn("content identity sets (files by repo, path and content hash; entities by id)", failed)
    self.assertNotIn("content rows", failed)

  def test_red_one_relationship_count_changed_in_t1(self):
    mutated = _result("T1", lambda e: _edit(e, "relationship_counts.tsv", replace=lambda l: "CONTAINS, 2849\n" if l.startswith("CONTAINS") else l))
    self.assertIn("relationship counts per type", _failed(checks_final.check_final_state(mutated, self.c, self.cf)))

  def test_red_the_sha1_full_row_dropped_from_t1(self):
    sha1 = self.c["final"]["shapes"][SCOPE]["sha_chain"][0]
    mutated = _result("T1", lambda e: _edit(e, "real_facts_chain.tsv", drop=lambda l: l.split("\t")[2] == sha1 and "\tcontent_entity:" in l))
    self.assertIn("the sha1 full generation of every scope equals C's strictly",
                  _failed(checks_final.check_final_state(mutated, self.c, self.cf)))

  def test_red_a_disallowed_repository_fact_key_differs_between_c_and_cf(self):
    def corrupt(export: str) -> None:
      _edit(export, "real_facts_chain.tsv", replace=lambda l: l.replace('"name": "fixture-02"', '"name": "renamed"') if "\trepository:" in l else l)
    mutated_cf = _result("CF", corrupt)
    checks = checks_final.check_cf_reference(self.c, mutated_cf)
    self.assertIn("the repository fact differs between C and CF only in the delta keys", _failed(checks))


if __name__ == "__main__":
  unittest.main()
