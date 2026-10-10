#!/usr/bin/env python3
"""Guard the opt-in H1 workflow and its failure artifact path."""

import argparse
from pathlib import Path
import re


parser = argparse.ArgumentParser()
parser.add_argument("--workflow", type=Path,
                    default=Path(".github/workflows/read-api-latency-gate.yml"))
args = parser.parse_args()
workflow = args.workflow.read_text(encoding="utf-8")
required = {
    "opt-in boolean": r"(?m)^\s+capture_h1_trace:\s*\n\s+type: boolean\s*\n\s+default: false\s*$",
    "pinned collector": r"otel/opentelemetry-collector-contrib:0\.143\.0@sha256:3bc07732530c87c53f9103b01a3afed972fdeba26087a590c1098781736e58c2",
    "conditional collector": r"(?s)- name: Start H1 diagnostic collector\n\s+if: .*capture_h1_trace.*\n",
    "diagnostic flag": r"GATE_H1_CAPTURE_DIR",
    "sampler": r"scripts/ci/h1-stage-sampler.sh",
    "collector stop": r"(?s)- name: Stop H1 diagnostic collector\n\s+if: .*always\(\).*capture_h1_trace.*\n",
    "failure archive": r"(?s)- name: Archive H1 diagnostic capture\n\s+if: .*always\(\).*capture_h1_trace.*\n",
    "default gate identity": r"GATE_CONCURRENT_WORKERS: 4\s*\n\s+GATE_CONCURRENT_REQUESTS: 20\s*\n\s+GATE_RUNS: 3",
}
missing = [name for name, pattern in required.items()
           if not re.search(pattern, workflow)]
if missing:
    raise SystemExit("H1 workflow wiring missing: " + ", ".join(missing))
print("PASS: H1 workflow wiring")
