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


def step(name: str) -> str:
    """Return one executable workflow step, excluding later steps and comments."""
    match = re.search(rf"(?ms)^      - name: {re.escape(name)}\n(.*?)(?=^      - name:|\Z)",
                      workflow)
    return match.group(1) if match else ""


collector = step("Start H1 diagnostic collector")
run_gate = step("Run scale and concurrent HTTP/MCP proof")
stop = step("Stop H1 diagnostic collector")
archive = step("Archive H1 diagnostic capture")
required = {
    "opt-in boolean": bool(re.search(
        r"(?m)^      capture_h1_trace:\s*\n\s+type: boolean\s*\n\s+default: false\s*$",
        workflow)),
    "conditional collector": "if: ${{ inputs.capture_h1_trace == true }}" in collector,
    "pinned collector": bool(re.search(
        r"(?s)docker run -d --name eshu-h1-collector.*?"
        r"otel/opentelemetry-collector-contrib:0\.143\.0@sha256:"
        r"3bc07732530c87c53f9103b01a3afed972fdeba26087a590c1098781736e58c2",
        collector)),
    "diagnostic gate flag": "export GATE_H1_CAPTURE_DIR=" in run_gate,
    "sampler": "bash scripts/ci/h1-stage-sampler.sh" in run_gate,
    "collector stop": "if: ${{ always() && inputs.capture_h1_trace == true }}" in stop,
    "failure archive": all(marker in archive for marker in (
        "if: ${{ always() && inputs.capture_h1_trace == true }}",
        "uses: actions/upload-artifact@v4",
        "path: ${{ runner.temp }}/methodology-h1/")),
    "default gate identity": bool(re.search(
        r"GATE_CONCURRENT_WORKERS: 4\s*\n\s+GATE_CONCURRENT_REQUESTS: 20\s*\n\s+GATE_RUNS: 3",
        workflow)),
}
missing = [name for name, present in required.items() if not present]
if missing:
    raise SystemExit("H1 workflow wiring missing: " + ", ".join(missing))
print("PASS: H1 workflow wiring")
