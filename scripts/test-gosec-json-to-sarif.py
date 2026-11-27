#!/usr/bin/env python3
"""Unit tests for scripts/lib/gosec-json-to-sarif.py."""

from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
CONVERTER = REPO_ROOT / "scripts/lib/gosec-json-to-sarif.py"


def run_converter(in_path: Path, out_path: Path, base_dir: Path | None = None) -> subprocess.CompletedProcess[str]:
    cmd = [sys.executable, str(CONVERTER), "--input", str(in_path), "--output", str(out_path)]
    if base_dir is not None:
        cmd.extend(["--base-dir", str(base_dir)])
    return subprocess.run(cmd, capture_output=True, text=True, check=False)


class GosecJsonToSarifTest(unittest.TestCase):
    def test_clean_report_produces_valid_empty_sarif(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            inp = root / "in.json"
            out = root / "out.sarif"
            inp.write_text(
                json.dumps(
                    {
                        "Golang errors": {},
                        "Issues": [],
                        "Stats": {"files": 10, "lines": 100, "nosec": 0, "found": 0},
                        "GosecVersion": "2.30.1",
                    }
                ),
                encoding="utf-8",
            )
            res = run_converter(inp, out, root)
            self.assertEqual(res.returncode, 0, res.stderr)
            sarif = json.loads(out.read_text(encoding="utf-8"))
            self.assertEqual(sarif["version"], "2.1.0")
            self.assertEqual(len(sarif["runs"]), 1)
            run = sarif["runs"][0]
            self.assertEqual(run["results"], [])
            self.assertEqual(run["tool"]["driver"]["name"], "gosec")
            self.assertEqual(run["tool"]["driver"]["version"], "2.30.1")
            self.assertNotIn("rules", run["tool"]["driver"])

    def test_findings_convert_with_relative_uri_and_line_ranges(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            go_file = root / "go/internal/foo/bar.go"
            go_file.parent.mkdir(parents=True)
            go_file.write_text("package foo\n", encoding="utf-8")
            inp = root / "in.json"
            out = root / "out.sarif"
            inp.write_text(
                json.dumps(
                    {
                        "Golang errors": {},
                        "Issues": [
                            {
                                "severity": "MEDIUM",
                                "confidence": "HIGH",
                                "cwe": {"id": "276", "url": "https://cwe.mitre.org/data/definitions/276.html"},
                                "rule_id": "G301",
                                "details": "Expect directory permissions to be 0750 or less",
                                "file": str(go_file),
                                "code": "5: func f() {\n6: \tos.MkdirAll(p, 0o755)\n7: }\n",
                                "line": "6",
                                "column": "9",
                            },
                            {
                                "severity": "LOW",
                                "confidence": "LOW",
                                "cwe": {"id": "276", "url": "https://cwe.mitre.org/data/definitions/276.html"},
                                "rule_id": "G304",
                                "details": "File path provided as taint input",
                                "file": str(go_file),
                                "code": "10: os.ReadFile(p)\n",
                                "line": "10-12",
                                "column": "4",
                            },
                        ],
                        "Stats": {"files": 1, "lines": 20, "nosec": 0, "found": 2},
                        "GosecVersion": "dev",
                    }
                ),
                encoding="utf-8",
            )
            res = run_converter(inp, out, root)
            self.assertEqual(res.returncode, 0, res.stderr)
            sarif = json.loads(out.read_text(encoding="utf-8"))
            run = sarif["runs"][0]
            self.assertEqual(len(run["results"]), 2)
            r0, r1 = run["results"]
            self.assertEqual(r0["ruleId"], "G301")
            self.assertEqual(r0["level"], "error")
            loc0 = r0["locations"][0]["physicalLocation"]
            self.assertEqual(loc0["artifactLocation"]["uri"], "go/internal/foo/bar.go")
            self.assertEqual(loc0["region"]["startLine"], 6)
            self.assertEqual(loc0["region"]["endLine"], 6)
            self.assertEqual(loc0["region"]["startColumn"], 9)
            self.assertEqual(loc0["region"]["snippet"]["text"], "os.MkdirAll(p, 0o755)")

            self.assertEqual(r1["ruleId"], "G304")
            self.assertEqual(r1["level"], "warning")
            loc1 = r1["locations"][0]["physicalLocation"]
            self.assertEqual(loc1["region"]["startLine"], 10)
            self.assertEqual(loc1["region"]["endLine"], 12)
            self.assertEqual(len(run["tool"]["driver"]["rules"]), 2)
            self.assertEqual(len(run["taxonomies"][0]["taxa"]), 1)

    def test_invalid_json_and_missing_fields_fail_closed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            out = root / "out.sarif"
            bad_json = root / "bad.json"
            bad_json.write_text("not json", encoding="utf-8")
            self.assertNotEqual(run_converter(bad_json, out, root).returncode, 0)
            self.assertFalse(out.exists())

            missing_keys = root / "missing.json"
            missing_keys.write_text(json.dumps({"Issues": []}), encoding="utf-8")
            self.assertNotEqual(run_converter(missing_keys, out, root).returncode, 0)
            self.assertFalse(out.exists())


if __name__ == "__main__":
    unittest.main()
