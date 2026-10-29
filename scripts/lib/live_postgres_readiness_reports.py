"""Emit, select, and verify exact readiness shard reports."""

from __future__ import annotations

from collections import Counter
from pathlib import Path
import re
import sys

from live_postgres_readiness_shards import (
    _account, _atomic_json, _parse_json, _read_manifest, _read_plan,
    _validate_head, partition_runs, REPORT_KEYS)
import live_postgres_readiness_weights as wt
import live_postgres_readiness_v2 as v2


def shard_ids(weights: wt.Weights | v2.WeightsV2) -> tuple[str, ...]:
    """Return the exact identifiers accepted under this model."""
    count = weights.shard_count if isinstance(weights, v2.WeightsV2) else wt.SHARD_COUNT
    return tuple(f"{number}/{count}" for number in range(1, count + 1))


ARTIFACT_RE = re.compile(r"^readiness-shard-\d+-(\d+)-[1-6]$")

def _report(head: str, shard: str, plan: Path, manifest_path: Path,
            output: Path, weights_path: Path | None = None) -> bool:
    """Emit one parseable report even when the manifest is incomplete."""
    expected: list[str] = []
    entries: list[dict[str, object]] = []
    errors: list[str] = []
    model: wt.Weights | v2.WeightsV2 | None = None
    try:
        _validate_head(head)
        model = wt.load_weights(weights_path)
        expected = partition_runs(_read_plan(plan), shard, model)
    except ValueError as error:
        errors.append(f"invalid report input: {error}")
    manifest_entries, manifest_errors = _read_manifest(manifest_path)
    entries.extend(manifest_entries)
    errors.extend(manifest_errors)
    runs: list[dict[str, object]] = []
    if expected:
        runs, account_errors = _account(expected, entries)
        errors.extend(account_errors)
    report: dict[str, object] = {
        "schema_version": 2 if isinstance(model, v2.WeightsV2) else 1,
        "head": head, "shard": shard,
        "ok": not errors, "expected_count": len(expected),
        "observed_count": sum(1 for run in runs if run["status"] == 0),
        "expected_runs": expected, "runs": runs, "errors": errors,
    }
    if isinstance(model, v2.WeightsV2):
        report["model_fingerprint"] = model.fingerprint()
    _atomic_json(output, report)
    for error in errors:
        print(f"readiness shard {shard}: {error}", file=sys.stderr)
    print(f"readiness shard {shard}: {report['observed_count']}/{len(expected)} "
          f"passing runs, ok={not errors}")
    return not errors


def _validate_report(value: object, plan: list[str], head: str,
                     seen_shards: set[str],
                     weights: wt.Weights | v2.WeightsV2 | None = None) -> tuple[list[str], str]:
    """Require one fresh matching report and return its assigned runs."""
    model = weights or wt.load_weights()
    required = REPORT_KEYS | ({"model_fingerprint"}
                              if isinstance(model, v2.WeightsV2) else set())
    if not isinstance(value, dict) or set(value) != required:
        raise ValueError("report schema mismatch")
    shard = value["shard"]
    if shard not in shard_ids(model) or shard in seen_shards:
        raise ValueError(f"invalid or duplicate shard: {shard!r}")
    seen_shards.add(shard)
    expected = partition_runs(plan, shard, model)
    version = 2 if isinstance(model, v2.WeightsV2) else 1
    if (type(value["schema_version"]) is not int
            or value["schema_version"] != version
            or value["head"] != head
            or (isinstance(model, v2.WeightsV2)
                and value["model_fingerprint"] != model.fingerprint())):
        raise ValueError(f"stale metadata for {shard}")
    if (value["expected_runs"] != expected
            or type(value["expected_count"]) is not int
            or value["expected_count"] != len(expected)):
        raise ValueError(f"plan mismatch for {shard}")
    raw_runs = value["runs"]
    if not isinstance(raw_runs, list):
        raise ValueError(f"invalid runs for {shard}")
    for entry in raw_runs:
        if (not isinstance(entry, dict) or set(entry) != {"run", "status"}
                or entry["run"] not in expected
                or entry["status"] not in (0, 1)):
            raise ValueError(f"invalid run entry for {shard}: {entry!r}")
    if (type(value["observed_count"]) is not int
            or type(value["ok"]) is not bool
            or not isinstance(value["errors"], list)
            or any(not isinstance(error, str) for error in value["errors"])):
        raise ValueError(f"invalid accounting for {shard}")
    ok_runs = [entry["run"] for entry in raw_runs if entry["status"] == 0]
    if sorted(ok_runs) != sorted(expected):
        raise ValueError(f"failed or incomplete shard {shard}: {value['errors']}")
    if value["observed_count"] != len(ok_runs):
        raise ValueError(f"miscounted shard {shard}: observed {value['observed_count']}")
    if not value["ok"] or value["errors"]:
        raise ValueError(f"failed or incomplete shard {shard}: {value['errors']}")
    return expected, shard  # type: ignore[return-value]


def _verify(head: str, plan_path: Path, report_paths: list[Path],
            weights: wt.Weights | v2.WeightsV2 | None = None) -> bool:
    """Require the model's reports to cover the canonical plan exactly."""
    _validate_head(head)
    plan = _read_plan(plan_path)
    model = weights or wt.load_weights()
    allowed = shard_ids(model)
    if len(report_paths) != len(allowed):
        paths = sorted(str(path) for path in report_paths)
        number = "four" if len(allowed) == 4 else str(len(allowed))
        raise ValueError(f"expected exactly {number} shard reports, "
                         f"got {len(report_paths)}: {paths}")
    seen_shards: set[str] = set()
    assigned: list[str] = []
    per_shard: dict[str, list[str]] = {}
    errors: list[str] = []
    for path in report_paths:
        try:
            expected, shard = _validate_report(
                _parse_json(path.read_text(encoding="utf-8")), plan, head,
                seen_shards, model)
            assigned.extend(expected)
            per_shard[shard] = expected
        except (OSError, ValueError, TypeError) as error:
            errors.append(f"{path}: {error}")
    if seen_shards != set(allowed):
        errors.append(f"missing shards: {sorted(set(allowed) - seen_shards)}")
    if isinstance(model, v2.WeightsV2):
        ordered = [run for shard in allowed for run in per_shard.get(shard, [])]
        if ordered != plan:
            errors.append("shard ordered union differs from canonical full plan")
    elif Counter(assigned) != Counter(plan):
        errors.append("shard union differs from canonical full plan")
    for error in errors:
        print(f"readiness aggregate: {error}", file=sys.stderr)
    print(f"readiness aggregate: {len(assigned)}/{len(plan)} planned runs, "
          f"shards={len(seen_shards)}/{len(allowed)}, ok={not errors}")
    return not errors


def _candidate_order(path: Path) -> tuple[int, int, str]:
    """Order rerun candidates newest first for one shard's report.

    A failed-job rerun uploads under a newer attempt while earlier
    attempts' reports stay downloadable, so recency comes from the
    artifact directory name before filesystem timestamps. Unparseable
    layouts sort oldest; the path breaks remaining ties deterministically.
    """
    attempt = 0
    match = ARTIFACT_RE.fullmatch(path.parent.name)
    if match:
        attempt = int(match.group(1))
    try:
        stamp = path.stat().st_mtime_ns
    except OSError:
        stamp = 0
    return (attempt, stamp, str(path))


def _select(head: str, plan_path: Path, report_paths: list[Path],
            output_dir: Path, weights: wt.Weights | v2.WeightsV2 | None = None) -> bool:
    """Copy the newest valid report per shard for a rerun-proof aggregate.

    Every candidate still passes the same head and plan validation as
    ``verify``; a rejected older attempt is only a diagnostic. A shard
    whose candidates are all stale or failed keeps the aggregate red, and
    a genuinely missing shard is still an error. Only one valid winner
    per required shard reaches ``verify``.
    """
    _validate_head(head)
    plan = _read_plan(plan_path)
    model = weights or wt.load_weights()
    allowed = shard_ids(model)
    contenders: dict[str, list[tuple[tuple[int, int, str], Path]]] = {}
    attempted: set[str] = set()
    for path in report_paths:
        claimed: object = None
        try:
            value = _parse_json(path.read_text(encoding="utf-8"))
            claimed = value.get("shard") if isinstance(value, dict) else None
            _, shard = _validate_report(value, plan, head, set(), model)
        except (OSError, ValueError, TypeError, AttributeError) as error:
            print(f"readiness select: rejected {path}: {error}", file=sys.stderr)
            if isinstance(claimed, str) and claimed in allowed:
                attempted.add(claimed)
            continue
        attempted.add(shard)
        contenders.setdefault(shard, []).append((_candidate_order(path), path))
    errors = []
    if missing := sorted(set(allowed) - attempted):
        errors.append(f"missing shards: {missing}")
    errors.extend(f"no valid report for shard {shard}"
                  for shard in sorted(attempted - set(contenders)))
    for error in errors:
        print(f"readiness select: {error}", file=sys.stderr)
    if errors:
        return False
    output_dir.mkdir(parents=True, exist_ok=True)
    for shard in allowed:
        _, winner = max(contenders[shard])
        target = output_dir / f"readiness-shard-{shard[0]}-report.json"
        try:
            target.write_bytes(winner.read_bytes())
        except OSError as error:
            print(f"readiness select: {target}: {error}", file=sys.stderr)
            return False
        print(f"readiness select: {shard} <- {winner}")
    print(f"readiness select: {len(allowed)}/{len(allowed)} shards selected, ok=True")
    return True
