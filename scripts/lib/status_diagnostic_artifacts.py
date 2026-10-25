#!/usr/bin/env python3
"""Validate bounded status diagnostic artifacts without exposing their contents."""

import argparse
import json
import math
import pathlib
from typing import cast


def _object(value: object) -> dict[str, object]:
    """Require a JSON object with string keys."""
    if not isinstance(value, dict) or any(not isinstance(k, str) for k in value):
        raise ValueError("invalid object")
    return cast(dict[str, object], value)


def _records(value: object, limit: int = 8192) -> list[dict[str, object]]:
    """Require a bounded array of JSON objects."""
    if not isinstance(value, list) or len(value) > limit:
        raise ValueError("invalid records")
    return [_object(item) for item in value]


def _identifier(value: object) -> str:
    """Require the server's canonical nonzero uint64 decimal identifier."""
    if (not isinstance(value, str) or not value or len(value) > 20
            or any(c < "0" or c > "9" for c in value)
            or value.startswith("0") or int(value) > 18446744073709551615):
        raise ValueError("invalid identifier")
    return value



def _keys(record: dict[str, object], required: set[str],
          optional: set[str] | None = None) -> None:
    """Reject omitted contract fields and arbitrary retained payload fields."""
    if not required <= record.keys() or record.keys() - required - (optional or set()):
        raise ValueError("invalid record fields")


def _interval(record: dict[str, object], end_key: str = "end_unix_nano") -> tuple[int, int]:
    """Require ordered positive integer wall-clock boundaries."""
    start, end = record.get("start_unix_nano"), record.get(end_key)
    if type(start) is not int or type(end) is not int:
        raise ValueError("invalid interval")
    start, end = cast(int, start), cast(int, end)
    if not 0 < start <= end:
        raise ValueError("invalid interval")
    return start, end


def _hex(value: object, width: int, allow_zero: bool = False) -> str:
    """Require canonical SDK trace or span identifiers."""
    if (not isinstance(value, str) or len(value) != width
            or any(c not in "0123456789abcdef" for c in value)
            or (not allow_zero and value == "0" * width)):
        raise ValueError("invalid trace identifier")
    return value


def _validate_spans(server: dict[str, object], requests: dict[str, dict[str, object]]) -> None:
    """Independently require sanitized, bounded, completed request span evidence."""
    selected = {"eshu.status_diagnostic_request", "eshu.status_diagnostic_dispatch",
                "eshu-api", "postgres.reader_access", "postgres.status_snapshot"}
    allowed = {
        "role": {"writer", "reader", "unknown"},
        "stage": {"writer_checkpoint", "reader_borrow", "reader_identity", "reader_replay",
                  "business_query", "transaction_control", "unknown"},
        "outcome": {"ok", "error", "deadline", "canceled", "panic", "unknown"},
        "phase": {"begin", "jit", "read", "commit", "rollback", "unknown"},
        "query_summary": {"scope_counts", "generation_counts", "generation_transitions",
                          "active_work_summary", "active_work_summary_model", "producer_activity",
                          "collector_generation_dead_letters", "coordinator", "registry_collectors",
                          "aws_cloud_scans", "aws_freshness", "infra_inventory", "vulnerability_sources",
                          "collector_fact_evidence", "terraform_state", "terraform_state_model",
                          "coordinator_model", "semantic_extraction"},
    }
    grouped: dict[str, list[dict[str, object]]] = {key: [] for key in requests}
    seen: set[tuple[str, str]] = set()
    for span in _records(server.get("spans")):
        _keys(span, {"request_id", "name", "span_id", "parent_span_id",
                     "start_unix_nano", "end_unix_nano"}, set(allowed))
        identifier = _identifier(span.get("request_id"))
        if identifier not in requests or span.get("name") not in tuple(selected):
            raise ValueError("unselected span")
        span_id = _hex(span.get("span_id"), 16)
        _hex(span.get("parent_span_id"), 16, allow_zero=True)
        key = (cast(str, requests[identifier]["trace_id"]), span_id)
        if key in seen:
            raise ValueError("duplicate span")
        seen.add(key)
        _interval(span)
        for field, values in allowed.items():
            if field in span and span[field] not in tuple(values):
                raise ValueError("unsanitized span attribute")
        grouped[identifier].append(span)
    for identifier, request in requests.items():
        spans = grouped[identifier]
        roots = [span for span in spans if span["name"] == "eshu.status_diagnostic_request"]
        servers = [span for span in spans if span["name"] == "eshu-api"]
        if len(roots) != 1 or len(servers) != 1:
            raise ValueError("missing root or server span")
        root = roots[0]
        if root["parent_span_id"] != "0" * 16 or servers[0]["parent_span_id"] != root["span_id"]:
            raise ValueError("detached server span")
        start, end = _interval(root)
        request_start, request_end = _interval(request)
        if start < request_start or end < request_end:
            raise ValueError("invalid root boundary")
        for span in spans:
            span_start, span_end = _interval(span)
            if span_start < start or span_end > end:
                raise ValueError("span outside request root")
        if request["status_code"] == 200:
            names = {cast(str, span["name"]) for span in spans}
            checkpoint = any(span.get("name") == "postgres.reader_access" and span.get("role") == "writer" and span.get("stage") == "writer_checkpoint" for span in spans)
            reader = any(span.get("name") == "postgres.reader_access" and span.get("role") == "reader" for span in spans)
            if (request["post_auth_reached"] is not True or not checkpoint or not reader
                    or not {"eshu.status_diagnostic_dispatch", "postgres.status_snapshot"} <= names):
                raise ValueError("missing successful request stages")

def validate_reports(server: dict[str, object], client: dict[str, object],
                     expected: int) -> None:
    """Require complete reports and exact client/server request correlation.

    Args:
        server: The sanitized API collector report.
        client: The latency gate's sanitized dispatch report.
        expected: The configured concurrent request count.

    Raises:
        ValueError: Evidence is missing, ambiguous, or inconsistent.
    """
    if type(expected) is not int or not 1 <= expected <= 4096:
        raise ValueError("invalid expected count")
    for report in (server, client):
        if (type(report.get("schema_version")) is not int
                or report["schema_version"] != 1
                or report.get("complete") is not True
                or report.get("errors") != []):
            raise ValueError("incomplete report")
    if server.get("overflow") is not False:
        raise ValueError("overflow report")
    _keys(server, {"schema_version", "complete", "overflow", "requests", "spans", "errors"})
    _keys(client, {"schema_version", "complete", "samples", "errors"})
    requests: dict[str, dict[str, object]] = {}
    for request in _records(server.get("requests")):
        _keys(request, {"id", "trace_id", "start_unix_nano", "end_unix_nano",
                        "status_code", "post_auth_reached"})
        _interval(request)
        _hex(request.get("trace_id"), 32)
        if type(request.get("post_auth_reached")) is not bool:
            raise ValueError("invalid auth marker")
        identifier = _identifier(request.get("id"))
        if identifier in requests:
            raise ValueError("duplicate server identifier")
        requests[identifier] = request
    if not 1 <= len(requests) <= 256:
        raise ValueError("invalid request count")
    _validate_spans(server, requests)
    seen: set[str] = set()
    positions: dict[str, set[int]] = {}
    for sample in _records(client.get("samples")):
        _keys(sample, {"server_id", "phase", "index", "status_code", "start_unix_nano",
                       "measured_end_unix_nano", "elapsed_ms"})
        client_start, client_end = _interval(sample, "measured_end_unix_nano")
        elapsed = sample.get("elapsed_ms")
        if (type(elapsed) not in (int, float) or not math.isfinite(cast(float, elapsed))
                or not math.isclose(cast(float, elapsed), (client_end - client_start) / 1e6,
                                    rel_tol=1e-9, abs_tol=1e-9)):
            raise ValueError("invalid measured interval")
        identifier = _identifier(sample.get("server_id"))
        phase, index = sample.get("phase"), sample.get("index")
        if phase not in ("warmup", "serial", "concurrent"):
            raise ValueError("invalid phase")
        if type(index) is not int or not 0 <= index < 4096:
            raise ValueError("invalid index")
        phase = cast(str, phase)
        index = cast(int, index)
        indexes = positions.setdefault(phase, set())
        if identifier in seen or index in indexes or identifier not in requests:
            raise ValueError("ambiguous correlation")
        request_start, request_end = _interval(requests[identifier])
        if client_end < request_start or request_end < client_start:
            raise ValueError("nonoverlapping request evidence")
        status = sample.get("status_code")
        if (type(status) is not int or not 100 <= status <= 599
                or type(requests[identifier].get("status_code")) is not int
                or requests[identifier]["status_code"] != status):
            raise ValueError("status mismatch")
        seen.add(identifier)
        indexes.add(index)
    if seen != set(requests) or positions.get("concurrent") != set(range(expected)):
        raise ValueError("missing correlation")



def _statement_rows(value: object) -> dict[tuple[int, int, int, bool], dict[str, object]]:
    """Require bounded numeric identities and counters without SQL text."""
    rows = _records(value, 10000)
    if len(rows) > 10000:
        raise ValueError("statement capacity exceeded")
    result: dict[tuple[int, int, int, bool], dict[str, object]] = {}
    for row in rows:
        _keys(row, {"dbid", "userid", "queryid", "toplevel", "calls", "rows", "blocks", "exec_ms", "stats_since_unix_micro"})
        if any(type(row[key]) is not int for key in ("dbid", "userid", "queryid", "calls", "rows", "blocks", "stats_since_unix_micro")):
            raise ValueError("invalid statement identity or counter")
        if cast(int, row["stats_since_unix_micro"]) <= 0 or cast(int, row["dbid"]) <= 0 or cast(int, row["userid"]) <= 0 or type(row["toplevel"]) is not bool:
            raise ValueError("invalid statement identity")
        elapsed = row["exec_ms"]
        if (any(cast(int, row[key]) < 0 for key in ("calls", "rows", "blocks"))
                or type(elapsed) not in (int, float) or not math.isfinite(cast(float, elapsed))
                or cast(float, elapsed) < 0):
            raise ValueError("invalid statement counter")
        key = (cast(int, row["dbid"]), cast(int, row["userid"]), cast(int, row["queryid"]), cast(bool, row["toplevel"]))
        if key in result:
            raise ValueError("duplicate statement identity")
        result[key] = row
    return result


def validate_statements(report: dict[str, object], client: dict[str, object]) -> None:
    """Require exact aggregate counter deltas outside concurrent client intervals."""
    _keys(report, {"schema_version", "complete", "scope", "snapshots", "deltas", "errors"})
    if (type(report["schema_version"]) is not int or report["schema_version"] != 1
            or report["complete"] is not True or report["errors"] != []
            or report["scope"] != "database aggregate; no per-request attribution"):
        raise ValueError("incomplete statement report")
    snapshots = _records(report["snapshots"])
    if len(snapshots) != 2:
        raise ValueError("missing statement boundary")
    for snapshot, phase in zip(snapshots, ("before_concurrent", "after_concurrent")):
        _keys(snapshot, {"phase", "start_unix_nano", "end_unix_nano", "records"})
        if snapshot["phase"] != phase:
            raise ValueError("invalid statement phase")
        _interval(snapshot)
    samples = [sample for sample in _records(client.get("samples")) if sample.get("phase") == "concurrent"]
    if not samples:
        raise ValueError("missing concurrent samples")
    intervals = [_interval(sample, "measured_end_unix_nano") for sample in samples]
    if (_interval(snapshots[0])[1] > min(start for start, _ in intervals)
            or _interval(snapshots[1])[0] < max(end for _, end in intervals)):
        raise ValueError("statement snapshot inside timed window")
    before, after = [_statement_rows(snapshot["records"]) for snapshot in snapshots]
    deltas = _statement_rows(report["deltas"])
    if not before.keys() <= after.keys() or after.keys() != deltas.keys():
        raise ValueError("missing statement identity")
    calls = 0
    for key, row in after.items():
        epoch = row["stats_since_unix_micro"]
        if (deltas[key]["stats_since_unix_micro"] != epoch
                or (key in before and before[key]["stats_since_unix_micro"] != epoch)):
            raise ValueError("statement collection epoch changed")
        for metric in ("calls", "rows", "blocks", "exec_ms"):
            prior = before.get(key, {}).get(metric, 0)
            expected = cast(float, row[metric]) - cast(float, prior)
            delta = cast(float, deltas[key][metric])
            if expected < 0 or (metric != "exec_ms" and expected != delta) or (metric == "exec_ms" and not math.isclose(expected, delta, rel_tol=1e-9, abs_tol=1e-9)):
                raise ValueError("invalid statement delta")
        calls += cast(int, deltas[key]["calls"])
    if calls < len(samples):
        raise ValueError("missing concurrent statement work")

def validate_plan(report: dict[str, object], statements: dict[str, object]) -> None:
    """Require sanitized supplementary execution after the last counter read."""
    _keys(report, {"schema_version", "complete", "scope", "source_sha256",
                   "as_of_unix_nano", "start_unix_nano", "end_unix_nano", "plan", "errors"})
    if (type(report["schema_version"]) is not int or report["schema_version"] != 1
            or report["complete"] is not True or report["errors"] != []
            or report["scope"] != "supplementary execution; no request attribution"):
        raise ValueError("incomplete supplementary plan")
    _hex(report["source_sha256"], 64)
    start, _ = _interval(report)
    as_of = report["as_of_unix_nano"]
    snapshots = _records(statements.get("snapshots"))
    if (len(snapshots) != 2 or type(as_of) is not int
            or not cast(int, snapshots[1]["end_unix_nano"]) <= cast(int, as_of) <= start):
        raise ValueError("plan inside measured window")
    plan = _object(report["plan"])
    _keys(plan, {"Plan", "Planning Time", "Execution Time"})
    for field in ("Planning Time", "Execution Time"):
        _plan_number(plan[field])
    count = [0]
    _validate_plan_node(plan["Plan"], 0, count)


def _plan_number(value: object) -> None:
    """Require finite nonnegative numeric plan evidence."""
    if type(value) not in (int, float) or not math.isfinite(cast(float, value)) or cast(float, value) < 0:
        raise ValueError("invalid plan metric")


def _validate_plan_node(value: object, depth: int, count: list[int]) -> None:
    """Validate a bounded closed operator tree without relation names or literals."""
    count[0] += 1
    if depth > 64 or count[0] > 2048:
        raise ValueError("plan tree capacity exceeded")
    node = _object(value)
    required = {"Node Type", "Actual Rows", "Actual Loops", "Actual Startup Time", "Actual Total Time"}
    buffers = {f"{location} {action} Blocks" for location in ("Shared", "Local")
               for action in ("Hit", "Read", "Dirtied", "Written")} | {"Temp Read Blocks", "Temp Written Blocks"}
    _keys(node, required, buffers | {"Plans"})
    operators = {"Result", "ProjectSet", "ModifyTable", "Append", "Merge Append", "Recursive Union",
                 "BitmapAnd", "BitmapOr", "Seq Scan", "Sample Scan", "Index Scan", "Index Only Scan",
                 "Bitmap Index Scan", "Bitmap Heap Scan", "Tid Scan", "Tid Range Scan", "Subquery Scan",
                 "Function Scan", "Table Function Scan", "Values Scan", "CTE Scan", "Named Tuplestore Scan",
                 "WorkTable Scan", "Foreign Scan", "Custom Scan", "Nested Loop", "Merge Join", "Hash Join",
                 "Materialize", "Memoize", "Sort", "Incremental Sort", "Group", "Aggregate", "WindowAgg",
                 "Unique", "Gather", "Gather Merge", "Hash", "SetOp", "LockRows", "Limit"}
    if node["Node Type"] not in tuple(operators):
        raise ValueError("invalid plan operator")
    for field in node.keys() - {"Node Type", "Plans"}:
        _plan_number(node[field])
    if "Plans" in node:
        for child in _records(node["Plans"], 2048):
            _validate_plan_node(child, depth + 1, count)


def validate_resources(value: object, client: dict[str, object]) -> None:
    """Require closed, monotonic resource samples covering concurrent requests."""
    records = _records(value, 3602)
    if len(records) < 4:
        raise ValueError("missing resource evidence")
    metadata, completion = records[0], records[-1]
    _keys(metadata, {"schema_version", "kind", "cadence_seconds", "clock_ticks_per_second",
                     "logical_cpus", "process_start_ticks", "scope", "cgroup_identity"})
    if (type(metadata["schema_version"]) is not int or metadata["schema_version"] != 1
            or metadata["kind"] != "metadata" or type(metadata["cadence_seconds"]) is not int
            or metadata["cadence_seconds"] != 1 or metadata["scope"] !=
            "process ticks exclude children; cgroup totals may include other processes"):
        raise ValueError("invalid resource metadata")
    for field in ("clock_ticks_per_second", "logical_cpus"):
        if type(metadata[field]) is not int or cast(int, metadata[field]) <= 0:
            raise ValueError("invalid resource configuration")
    roles = {"api", "postgres_postmaster"}
    identities, cgroups = _object(metadata["process_start_ticks"]), _object(metadata["cgroup_identity"])
    _keys(identities, roles)
    _keys(cgroups, roles)
    for role in roles:
        if type(identities[role]) is not int or cast(int, identities[role]) <= 0:
            raise ValueError("invalid process identity")
        _hex(cgroups[role], 64)
    samples = records[1:-1]
    _keys(completion, {"kind", "complete", "samples", "late_gap"})
    if (completion["kind"] != "completion" or completion["complete"] is not True
            or completion["late_gap"] is not False or type(completion["samples"]) is not int
            or completion["samples"] != len(samples) or len(samples) >= 3600):
        raise ValueError("incomplete resource evidence")
    previous: dict[str, dict[str, object]] = {}
    for index, sample in enumerate(samples):
        _keys(sample, {"kind", "gap_nano", "processes"})
        gap = sample["gap_nano"]
        if (sample["kind"] != "sample" or type(gap) is not int
                or not (0 if index == 0 else 1) <= cast(int, gap) <= 2000000000):
            raise ValueError("invalid resource cadence")
        processes = _object(sample["processes"])
        _keys(processes, roles)
        for role in roles:
            current = _object(processes[role])
            _keys(current, {"unix_nano", "monotonic_nano", "read_elapsed_nano",
                            "process_cpu_ticks", "cgroup_cpu", "cgroup_memory_bytes"})
            for field in current.keys() - {"cgroup_cpu"}:
                if type(current[field]) is not int or cast(int, current[field]) < 0:
                    raise ValueError("invalid resource counter")
            if (cast(int, current["unix_nano"]) <= 0 or cast(int, current["monotonic_nano"]) <= 0
                    or cast(int, current["read_elapsed_nano"]) > 1000000000):
                raise ValueError("stalled resource read")
            cpu = _object(current["cgroup_cpu"])
            _keys(cpu, {"usage_usec", "user_usec", "system_usec"})
            for metric in cpu.values():
                if type(metric) is not int or cast(int, metric) < 0:
                    raise ValueError("invalid resource CPU counter")
            if role in previous:
                prior = previous[role]
                if (cast(int, current["unix_nano"]) <= cast(int, prior["unix_nano"])
                        or not 0 < cast(int, current["monotonic_nano"]) - cast(int, prior["monotonic_nano"]) <= 2000000000
                        or cast(int, current["process_cpu_ticks"]) < cast(int, prior["process_cpu_ticks"])
                        or any(cast(int, cpu[key]) < cast(int, _object(prior["cgroup_cpu"])[key]) for key in cpu)):
                    raise ValueError("decreasing resource evidence")
            previous[role] = current
    intervals = [_interval(sample, "measured_end_unix_nano") for sample in _records(client.get("samples"))
                 if sample.get("phase") == "concurrent"]
    if not intervals:
        raise ValueError("missing concurrent resource window")
    for role in roles:
        first = _object(_object(samples[0]["processes"])[role])
        last = _object(_object(samples[-1]["processes"])[role])
        if (cast(int, first["unix_nano"]) > min(start for start, _ in intervals)
                or cast(int, last["unix_nano"]) < max(end for _, end in intervals)):
            raise ValueError("resource samples do not cover requests")


def main() -> int:
    """Validate report files and emit only a closed success or failure message."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("server", type=pathlib.Path)
    parser.add_argument("client", type=pathlib.Path)
    parser.add_argument("--expected", type=int, required=True)
    parser.add_argument("--statements", type=pathlib.Path)
    parser.add_argument("--plan", type=pathlib.Path)
    parser.add_argument("--resources", type=pathlib.Path)
    args = parser.parse_args()
    try:
        server = _object(json.loads(args.server.read_text(encoding="utf-8")))
        client = _object(json.loads(args.client.read_text(encoding="utf-8")))
        validate_reports(server, client, args.expected)
        if args.resources is not None:
            validate_resources([json.loads(line) for line in args.resources.read_text(encoding="utf-8").splitlines()], client)
        if args.statements is not None:
            statements = _object(json.loads(args.statements.read_text(encoding="utf-8")))
            validate_statements(statements, client)
            if args.plan is not None:
                validate_plan(_object(json.loads(args.plan.read_text(encoding="utf-8"))), statements)
        elif args.plan is not None:
            raise ValueError("plan requires statement boundaries")
    except (OSError, ValueError, TypeError):
        print("FAIL: status diagnostic artifacts are incomplete or inconsistent")
        return 1
    print("PASS: status diagnostic artifacts correlate completely")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
