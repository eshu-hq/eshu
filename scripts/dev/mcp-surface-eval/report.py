"""Renders the #8760 evaluation results as Markdown tables.

The tables carry corpus ids, family labels, expected tool names, and numbers.
They never carry question text, repository names, or any other entity from the
deployment, so the output is safe to paste into the evidence note.

Usage:
    python3 report.py results_claude.json results_codex.json > tables.md

Pass result files from one shape at a time. Keep dual and structured runs in
separate private output directories; the report refuses to combine them.
"""

from __future__ import annotations

import collections
import json
import pathlib
import sys
from typing import Any

import score

SURFACES = score.SURFACES
_FIELDS = ("task_id", "family", "tool_calls", "domain_calls", "wrong_tool_calls",
           "first_domain_call_correct", "success", "wall_seconds")
_OPTIONAL = ("cost_usd", "input_tokens", "output_tokens", "helper_calls",
             "sibling_wrong_calls", "off_family_wrong_calls", "overflowed_calls")


def _scores(rows: list[dict[str, Any]]) -> list[score.Score]:
    """Converts stored result rows back into Score objects."""
    return [score.Score(**{k: r[k] for k in _FIELDS},
                        **{k: r[k] for k in _OPTIONAL if k in r}) for r in rows]


def _by_surface(data: dict[str, Any], client: str) -> dict[str, list[dict]]:
    out: dict[str, list[dict]] = collections.defaultdict(list)
    for row in data[client]["results"]:
        out[row["surface"]].append(row)
    return out


def _cost(value: float) -> str:
    """Formats a mean cost; zero means the client reports no cost."""
    return f"{value:.3f}" if value else "n/a"


def summary_table(data: dict[str, Any]) -> str:
    """Returns the per client and surface summary as a Markdown table.

    Args:
        data: Parsed result files keyed by client name.

    Returns:
        A Markdown table with one row per client and surface.
    """
    head = ("| Client | Surface | Runs | Mean tool calls | Genuine wrong-pick "
            "rate | Helper-call rate | First call correct | Expected tool "
            "answered | Median wall s | Mean input tokens | Mean cost USD |\n"
            "|---|---|---|---|---|---|---|---|---|---|---|")
    lines = [head]
    for client in sorted(data):
        grouped = _by_surface(data, client)
        for surface in SURFACES:
            rows = grouped.get(surface)
            if not rows:
                continue
            s = score.summarize(_scores(rows))
            lines.append(
                f"| {client} | {surface} | {int(s['tasks'])} | "
                f"{s['mean_tool_calls']:.2f} | {s['genuine_wrong_rate']:.0%} | "
                f"{s['helper_rate']:.0%} | "
                f"{s['first_try_rate']:.0%} | {s['success_rate']:.0%} | "
                f"{s['median_wall_seconds']:.1f} | "
                f"{s['mean_input_tokens']:.0f} | {_cost(s['mean_cost_usd'])} |")
    return "\n".join(lines)


def task_table(data: dict[str, Any], expected: dict[str, str]) -> str:
    """Returns the per-task table: successes out of runs, calls, wrong calls.

    Args:
        data: Parsed result files keyed by client name.
        expected: Expected tool by corpus id, for the table's second column.

    Returns:
        A Markdown table with one row per task.
    """
    clients = sorted(data)
    cols = [(c, s) for c in clients for s in SURFACES]
    head = ("| Task | Expected tool | " +
            " | ".join(f"{c} {s}" for c, s in cols) + " |\n|---|---|" +
            "---|" * len(cols))
    cell: dict[tuple[str, str, str], list[dict]] = collections.defaultdict(list)
    ids: dict[str, str] = {}
    for client in clients:
        for row in data[client]["results"]:
            cell[(client, row["surface"], row["task_id"])].append(row)
            ids[row["task_id"]] = row["family"]
    lines = [head]
    for task_id in sorted(ids):
        parts = []
        for client, surface in cols:
            rows = cell.get((client, surface, task_id), [])
            if not rows:
                parts.append("-")
                continue
            ok = sum(1 for r in rows if r["success"])
            calls = sum(r["tool_calls"] for r in rows) / len(rows)
            wrong = sum(r["wrong_tool_calls"] for r in rows) / len(rows)
            parts.append(f"{ok}/{len(rows)} ok, {calls:.1f} calls, {wrong:.1f} wrong")
        lines.append(f"| {task_id} | {expected.get(task_id, '?')} | " +
                     " | ".join(parts) + " |")
    return "\n".join(lines)


def clients_table(data: dict[str, Any]) -> str:
    """Returns the client, shape, version, and model table for the evidence note."""
    lines = ["| Client | Shape | Version | Model |", "|---|---|---|---|"]
    for client in sorted(data):
        meta = data[client]["meta"]
        lines.append(f"| {client} | {meta.get('shape', 'server_default')} | "
                     f"{meta.get('version', '?')} | "
                     f"{meta.get('model', '?')} |")
    return "\n".join(lines)


def load_results(path: pathlib.Path) -> dict[str, Any]:
    """Loads a result file and validates the fields used by the report.

    Args:
        path: Result file to read.

    Returns:
        The parsed result envelope.

    Raises:
        SystemExit: If the file is unreadable or its envelope is malformed.
    """
    try:
        loaded = json.loads(path.read_text())
    except (OSError, ValueError) as exc:
        raise SystemExit(f"cannot read results file {path}") from exc
    if not isinstance(loaded, dict):
        raise SystemExit(f"invalid results envelope in {path}")
    meta = loaded.get("meta")
    if not isinstance(meta, dict):
        raise SystemExit(f"invalid results metadata in {path}")
    client = meta.get("client")
    if not isinstance(client, str) or not client.strip():
        raise SystemExit(f"invalid results client in {path}")
    if meta.get("shape", "server_default") not in (
            "server_default", "dual", "structured"):
        raise SystemExit(f"invalid results shape in {path}")
    if not isinstance(loaded.get("results"), list):
        raise SystemExit(f"invalid results envelope in {path}")
    if not all(score.valid_result_row(row) for row in loaded["results"]):
        raise SystemExit(f"invalid results row in {path}")
    return loaded


def main(argv: list[str]) -> int:
    """Loads result files named on the command line and prints all tables."""
    data: dict[str, Any] = {}
    shapes: set[str] = set()
    for path in argv:
        loaded = load_results(pathlib.Path(path))
        client = loaded["meta"]["client"]
        if client in data:
            raise SystemExit(
                f"duplicate {client} result files: report one shape at a time "
                "from separate private --out directories")
        shapes.add(loaded["meta"].get("shape", "server_default"))
        if len(shapes) > 1:
            raise SystemExit(
                "result files have different shapes: report one shape at a time "
                "from separate private --out directories")
        data[client] = loaded
    corpus = json.loads((pathlib.Path(__file__).resolve().parents[3] /
                         "go/internal/mcp/router/eval/testdata/corpus.json"
                         ).read_text())["questions"]
    expected = {q["id"]: q["expected"] for q in corpus}
    print("## Clients\n\n" + clients_table(data))
    print("\n## Summary by surface\n\n" + summary_table(data))
    print("\n## Per task\n\n" + task_table(data, expected))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
