# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
"""Proof that write_manifest.py writes the manifest the arm heredoc used to write.

The expected dictionaries are the fields of two real manifests of the matrix (a T1 rep with
a selection base and a C rep without), with the sha256 map of the binaries computed from a
temporary directory. RED: a field passed as a different type or a missing variable must
not produce the same JSON or must fail.

Run: python3 -I scripts/dev/10882-replay/test_write_manifest.py
"""
import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "write_manifest.py")

T1 = {
    "a_projector_generations": 48, "a_reducer_rows": 13000, "a_shape": "takeable", "arm": "T1", "b_cadence": "1s",
    "b_generations": 150, "commit": "3980a07cadc512ed8024d441bd781fa59105984b", "completion_step": True,
    "engaged_window_s": 120,
    "global_admission": "ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK=1000000,ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK=1000000",
    "global_marks": "raised", "load1_at_start": "2.25", "neo4j": "ghcr.io/eshu-hq/ci-neo4j-community@sha256:eabfbb04",
    "postgres": "postgres:18.6", "rep": 1, "selection_base_s": 20, "source_a": "git",
    "source_b": "dev.eshu.collector.synth-8877-cutover", "started": "2026-11-23T19:11:45Z",
    "thresholds": "ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE=1h,ESHU_SOURCE_BACKPRESSURE_RELEASE_AGE=15m", "ttl": "0s",
    "workers": {"ingester_projector": 4, "projector": 4},
}
CONTROL = dict(T1, arm="C", completion_step=False, selection_base_s=None, thresholds="", ttl="0s", rep=2)


def env_for(fields: dict, bindir: str) -> dict:
  env = {k: v for k, v in os.environ.items() if not k.startswith("REPLAY_MF_")}
  scalar = {
      "ARM": fields["arm"], "REP": fields["rep"], "TTL": fields["ttl"], "COMPLETE": int(fields["completion_step"]),
      "THRESHOLDS": fields["thresholds"], "GLOBAL_ADMISSION": fields["global_admission"], "GLOBAL_MARKS": fields["global_marks"],
      "SOURCE_A": fields["source_a"], "SOURCE_B": fields["source_b"], "A_SHAPE": fields["a_shape"],
      "A_REDUCER_ROWS": fields["a_reducer_rows"], "A_PROJECTOR_GENERATIONS": fields["a_projector_generations"],
      "B_GENERATIONS": fields["b_generations"], "B_CADENCE": fields["b_cadence"],
      "WORKERS_PROJECTOR": fields["workers"]["projector"], "WORKERS_INGESTER_PROJECTOR": fields["workers"]["ingester_projector"],
      "ENGAGED_WINDOW_S": fields["engaged_window_s"],
      "SELECTION_BASE_S": "None" if fields["selection_base_s"] is None else fields["selection_base_s"],
      "LOAD1_AT_START": fields["load1_at_start"], "POSTGRES": fields["postgres"], "NEO4J": fields["neo4j"],
      "COMMIT": fields["commit"], "STARTED": fields["started"], "BIN": bindir,
  }
  env.update({"REPLAY_MF_" + k: str(v) for k, v in scalar.items()})
  return env


class WriteManifestTest(unittest.TestCase):

  def _run(self, fields: dict, drop: str = "") -> tuple:
    with tempfile.TemporaryDirectory() as tmp:
      bindir = os.path.join(tmp, "bin")
      os.makedirs(bindir)
      for name, body in (("eshu-ingester", b"ing"), ("eshu-projector", b"proj"), ("replayseed.test", b"skip me")):
        with open(os.path.join(bindir, name), "wb") as handle:
          handle.write(body)
      env = env_for(fields, bindir)
      if drop:
        del env["REPLAY_MF_" + drop]
      out = os.path.join(tmp, "manifest.json")
      proc = subprocess.run([sys.executable, "-I", SCRIPT, out], env=env, capture_output=True, text=True, check=False)
      manifest = json.load(open(out, encoding="utf-8")) if os.path.exists(out) else None
      return proc.returncode, manifest

  def test_manifest_equals_the_fields_with_binary_hashes_of_eshu_binaries_only(self):
    for fields in (T1, CONTROL):
      code, manifest = self._run(fields)
      self.assertEqual(code, 0)
      want = dict(fields, binaries={"eshu-ingester": hashlib.sha256(b"ing").hexdigest(),
                                    "eshu-projector": hashlib.sha256(b"proj").hexdigest()})
      self.assertEqual(manifest, want)

  def test_types_are_those_of_the_old_heredoc(self):
    _, manifest = self._run(T1)
    self.assertIs(manifest["completion_step"], True)
    self.assertIsInstance(manifest["rep"], int)
    self.assertIsInstance(manifest["selection_base_s"], int)
    _, control = self._run(CONTROL)
    self.assertIsNone(control["selection_base_s"])

  def test_red_a_missing_field_fails_and_writes_nothing(self):
    code, manifest = self._run(T1, drop="TTL")
    self.assertNotEqual(code, 0)
    self.assertIsNone(manifest)


if __name__ == "__main__":
  unittest.main()
