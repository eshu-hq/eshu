#!/usr/bin/env python3
"""Convert gosec -fmt=json output to SARIF 2.1.0 for GitHub Code Scanning.

Allows security-scan.yml to run one fail-closed gosec pass (explicit package
dirs via precommit-go.sh gosec-all, checking Stats.files and Golang errors)
while still uploading a valid SARIF 2.1.0 report to GitHub Security tab.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import uuid
from pathlib import Path

SARIF_SCHEMA = (
    "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/"
    "sarif-2.1/schema/sarif-schema-2.1.0.json"
)
GOSEC_GUID = "8b518d5f-906d-39f9-894b-d327b1a421c5"
CWE_GUID = "f2856fc0-85b7-373f-83e7-6f8582243547"
CWE_NAMESPACE = uuid.UUID(CWE_GUID)
LINE_PREFIX_RE = re.compile(r"^\s*(\d+):\s?(.*)$")


def parse_positive_int(raw: object, default: int = 1) -> int:
    text = str(raw or "").strip()
    if not text:
        return default
    part = text.split("-", 1)[0].strip()
    try:
        val = int(part)
        return val if val >= 1 else default
    except ValueError:
        return default


def parse_line_range(raw: object) -> tuple[int, int]:
    text = str(raw or "").strip()
    if not text:
        return 1, 1
    if "-" in text:
        start_s, end_s = text.split("-", 1)
        start = parse_positive_int(start_s, 1)
        end = parse_positive_int(end_s, start)
        return start, max(start, end)
    start = parse_positive_int(text, 1)
    return start, start


def severity_to_level(severity: str) -> str:
    upper = severity.strip().upper()
    if upper in ("HIGH", "MEDIUM"):
        return "error"
    if upper == "LOW":
        return "warning"
    return "note"


def confidence_to_precision(confidence: str) -> str:
    lower = confidence.strip().lower()
    if lower in ("high", "medium", "low"):
        return lower
    return "medium"


def extract_snippet(code: str, start_line: int) -> str:
    if not code:
        return ""
    for raw_line in code.splitlines():
        match = LINE_PREFIX_RE.match(raw_line)
        if match and int(match.group(1)) == start_line:
            return match.group(2).strip()
    return code.strip()


def relative_uri(file_path: str, base_dir: Path | None) -> str:
    raw = file_path.strip()
    if not raw:
        raise ValueError("gosec issue missing non-empty file path")
    if base_dir is not None:
        try:
            rel = Path(raw).resolve().relative_to(base_dir.resolve())
            return rel.as_posix()
        except (ValueError, OSError):
            pass
    return raw.lstrip("/").replace("\\", "/")


def convert(report: dict[str, object], base_dir: Path | None) -> dict[str, object]:
    if not isinstance(report, dict):
        raise ValueError("gosec JSON root must be an object")
    issues = report.get("Issues")
    stats = report.get("Stats")
    golang_errors = report.get("Golang errors")
    if not isinstance(issues, list) or not isinstance(stats, dict) or not isinstance(golang_errors, dict):
        raise ValueError("gosec JSON missing required Issues, Stats, or Golang errors fields")

    version = str(report.get("GosecVersion") or "dev")
    rules_by_id: dict[str, dict[str, object]] = {}
    taxa_by_id: dict[str, dict[str, object]] = {}
    results: list[dict[str, object]] = []

    for issue in issues:
        if not isinstance(issue, dict):
            raise ValueError("gosec issue entry must be an object")
        rule_id = str(issue.get("rule_id") or "GOSEC")
        details = str(issue.get("details") or rule_id)
        severity = str(issue.get("severity") or "MEDIUM").upper()
        confidence = str(issue.get("confidence") or "MEDIUM").upper()
        level = severity_to_level(severity)
        start_line, end_line = parse_line_range(issue.get("line"))
        col = parse_positive_int(issue.get("column"), 1)
        uri = relative_uri(str(issue.get("file") or ""), base_dir)
        snippet = extract_snippet(str(issue.get("code") or ""), start_line)

        cwe = issue.get("cwe")
        cwe_id = ""
        cwe_url = ""
        if isinstance(cwe, dict):
            cwe_id = str(cwe.get("id") or "").strip()
            cwe_url = str(cwe.get("url") or "").strip()

        if cwe_id and cwe_id not in taxa_by_id:
            taxa_by_id[cwe_id] = {
                "guid": str(uuid.uuid5(CWE_NAMESPACE, f"CWE-{cwe_id}")),
                "helpUri": cwe_url or f"https://cwe.mitre.org/data/definitions/{cwe_id}.html",
                "id": cwe_id,
                "shortDescription": {"text": f"CWE-{cwe_id}"},
            }

        if rule_id not in rules_by_id:
            rule_obj: dict[str, object] = {
                "defaultConfiguration": {"level": level},
                "fullDescription": {"text": details},
                "help": {
                    "text": f"{details}\nSeverity: {severity}\nConfidence: {confidence}\n"
                },
                "id": rule_id,
                "name": f"CWE-{cwe_id}" if cwe_id else rule_id,
                "properties": {
                    "precision": confidence_to_precision(confidence),
                    "tags": ["security", severity],
                },
                "shortDescription": {"text": details},
            }
            if cwe_id:
                rule_obj["relationships"] = [
                    {
                        "kinds": ["superset"],
                        "target": {
                            "guid": taxa_by_id[cwe_id]["guid"],
                            "id": cwe_id,
                            "toolComponent": {"guid": CWE_GUID, "name": "CWE"},
                        },
                    }
                ]
            rules_by_id[rule_id] = rule_obj

        region: dict[str, object] = {
            "endColumn": col,
            "endLine": end_line,
            "sourceLanguage": "go",
            "startColumn": col,
            "startLine": start_line,
        }
        if snippet:
            region["snippet"] = {"text": snippet}

        results.append(
            {
                "level": level,
                "locations": [
                    {
                        "physicalLocation": {
                            "artifactLocation": {"uri": uri},
                            "region": region,
                        }
                    }
                ],
                "message": {"text": details},
                "ruleId": rule_id,
            }
        )

    taxonomy: dict[str, object] = {
        "downloadUri": "https://cwe.mitre.org/data/xml/cwec_v4.4.xml.zip",
        "guid": CWE_GUID,
        "informationUri": "https://cwe.mitre.org/data/published/cwe_v4.4.pdf/",
        "isComprehensive": True,
        "language": "en",
        "minimumRequiredLocalizedDataSemanticVersion": "4.4",
        "name": "CWE",
        "organization": "MITRE",
        "releaseDateUtc": "2021-03-15",
        "shortDescription": {"text": "The MITRE Common Weakness Enumeration"},
        "version": "4.4",
    }
    if taxa_by_id:
        taxonomy["taxa"] = [taxa_by_id[k] for k in sorted(taxa_by_id)]

    driver: dict[str, object] = {
        "guid": GOSEC_GUID,
        "informationUri": "https://github.com/securego/gosec/",
        "name": "gosec",
        "semanticVersion": version,
        "supportedTaxonomies": [{"guid": CWE_GUID, "name": "CWE"}],
        "version": version,
    }
    if rules_by_id:
        driver["rules"] = [rules_by_id[k] for k in sorted(rules_by_id)]

    return {
        "$schema": SARIF_SCHEMA,
        "version": "2.1.0",
        "runs": [
            {
                "results": results,
                "taxonomies": [taxonomy],
                "tool": {"driver": driver},
            }
        ],
    }


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, type=Path, help="gosec -fmt=json input path")
    parser.add_argument("--output", required=True, type=Path, help="SARIF 2.1.0 output path")
    parser.add_argument("--base-dir", type=Path, default=None, help="Repository root for relative URIs")
    args = parser.parse_args(argv)

    try:
        raw = json.loads(args.input.read_text(encoding="utf-8"))
        sarif = convert(raw, args.base_dir)
    except (OSError, json.JSONDecodeError, ValueError) as exc:
        print(f"gosec-json-to-sarif: {exc}", file=sys.stderr)
        return 1

    args.output.parent.mkdir(parents=True, exist_ok=True)
    tmp = args.output.with_suffix(args.output.suffix + f".tmp.{os.getpid()}")
    tmp.write_text(json.dumps(sarif, indent=2) + "\n", encoding="utf-8")
    os.replace(tmp, args.output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
