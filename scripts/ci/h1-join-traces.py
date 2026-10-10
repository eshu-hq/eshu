#!/usr/bin/env python3
"""Join hosted H1 client records to API server roots or report unavailable."""

import calendar
from datetime import datetime
import ipaddress
import json
import re
from pathlib import Path
import sys


STATUS_PATH = "/api/v0/status/ingesters/repository"


def utc_ns(value: str) -> int:
    """Parse an RFC3339 UTC stamp while retaining nanoseconds."""
    stamp = value.removesuffix("Z")
    whole, dot, fraction = stamp.partition(".")
    seconds = calendar.timegm(datetime.strptime(whole, "%Y-%m-%dT%H:%M:%S").timetuple())
    return seconds * 1_000_000_000 + int((fraction if dot else "").ljust(9, "0")[:9])


def span_attributes(span: dict[str, object]) -> dict[str, object]:
    """Flatten scalar OTLP attributes without retaining unrelated values."""
    result = {}
    for item in span.get("attributes", []):
        if item.get("key") in {"url.path", "network.peer.address", "network.peer.port",
                               "http.request.method", "http.response.status_code"}:
            result[item["key"]] = next(iter(item.get("value", {}).values()), None)
    return result


def roots(trace_file: Path) -> list[dict[str, object]]:
    """Read only eshu-api GET server roots from the collector export."""
    result = []
    with trace_file.open(encoding="utf-8") as lines:
        for line in lines:
            for resource in json.loads(line).get("resourceSpans", []):
                for scope in resource.get("scopeSpans", []):
                    for span in scope.get("spans", []):
                        if span.get("name") != "eshu-api" or span.get("kind") != 2:
                            continue
                        attrs = span_attributes(span)
                        if attrs.get("http.request.method") != "GET" or attrs.get("url.path") not in {STATUS_PATH, "/readyz"}:
                            continue
                        result.append({
                            "path": attrs["url.path"],
                            "peer_ip": attrs.get("network.peer.address"),
                            "peer_port": int(attrs.get("network.peer.port", 0)),
                            "status": int(attrs.get("http.response.status_code", 0)),
                            "trace_id": span.get("traceId"),
                            "start_ns": int(span["startTimeUnixNano"]),
                            "end_ns": int(span["endTimeUnixNano"]),
                        })
    return result


def join(capture_dir: Path) -> dict[str, object]:
    """Require one to one attribution of the readiness canary and 20 GETs."""
    trace_roots = roots(capture_dir / "traces.jsonl")
    marker_start = utc_ns((capture_dir / "get_start.txt").read_text().strip())
    marker_end = utc_ns((capture_dir / "get_end.txt").read_text().strip())
    status_roots = [root for root in trace_roots if root["path"] == STATUS_PATH
                    and marker_start <= root["start_ns"] <= marker_end]
    records = json.loads((capture_dir / "httptrace.json").read_text())["records"]
    records = [record for record in records if record["route"] == "GET /api/v0/status/ingesters/{ingester}"]
    if len(records) != 20 or len(status_roots) != 20:
        raise ValueError(f"expected 20 client/server GETs, got {len(records)}/{len(status_roots)}")
    matched = set()
    joins = []
    for index, record in enumerate(sorted(records, key=lambda entry: entry["request_index"])):
        if record["request_index"] != index or record["status"] != 200:
            raise ValueError("client indexes/status incomplete")
        start = utc_ns(record["start_utc"])
        end = utc_ns(record["end_utc"])
        events = {event["name"]: start + round(event["at_ms"] * 1_000_000)
                  for event in record["events"]}
        if "got_conn" not in events or "first_response_byte" not in events:
            raise ValueError(f"client {index} lacks connection events")
        low, high = events["got_conn"], events["first_response_byte"]
        if high < low or end < start:
            raise ValueError(f"client {index} has invalid timing")
        try:
            local_ip = ipaddress.ip_address(record["local_ip"])
        except ValueError as error:
            raise ValueError(f"client {index} has invalid local IP") from error
        candidates = [(i, root) for i, root in enumerate(status_roots)
                      if root["status"] == 200
                      and ipaddress.ip_address(root["peer_ip"]) == local_ip
                      and root["peer_port"] == record["local_port"]
                      and low <= root["start_ns"] <= high
                      and root["end_ns"] >= start and root["start_ns"] <= end]
        if len(candidates) != 1 or candidates[0][0] in matched:
            raise ValueError(f"client {index} has {len(candidates)} unique server matches")
        root_index, root = candidates[0]
        matched.add(root_index)
        joins.append({"request_index": index, "trace_id": root["trace_id"],
                      "client_ms": record["sample_ms"],
                      "server_ms": (root["end_ns"] - root["start_ns"]) / 1_000_000})
    trace_ids = [entry["trace_id"] for entry in joins]
    if (len(matched) != 20 or len(set(trace_ids)) != 20
            or any(not re.fullmatch(r"[0-9a-f]{32}", trace_id) for trace_id in trace_ids)):
        raise ValueError("server roots or trace IDs are missing or duplicated")
    canary = dict(line.split("=", 1) for line in (capture_dir / "readyz-canary.txt").read_text().splitlines())
    canary_ip, canary_port = canary["local_endpoint"].rsplit(" ", 1)
    canary_start, canary_end = utc_ns(canary["start_utc"]), utc_ns(canary["end_utc"])
    canary_matches = [root for root in trace_roots if root["path"] == "/readyz"
                      and root["status"] == 200
                      and ipaddress.ip_address(root["peer_ip"]) == ipaddress.ip_address(canary_ip)
                      and root["peer_port"] == int(canary_port)
                      and root["start_ns"] <= canary_end and root["end_ns"] >= canary_start]
    if len(canary_matches) != 1:
        raise ValueError(f"readiness canary has {len(canary_matches)} matches")
    return {"status": "joined", "readyz_trace_id": canary_matches[0]["trace_id"],
            "client_server_joins": joins}


def main() -> int:
    """Write an explicit success or unavailable verdict without leaking traces."""
    capture_dir = Path(sys.argv[1])
    try:
        result = join(capture_dir)
    except (OSError, ValueError, KeyError, TypeError, json.JSONDecodeError) as error:
        result = {"status": "unavailable", "reason": str(error)}
    (capture_dir / "join-result.json").write_text(json.dumps(result, indent=2) + "\n")
    return 0 if result["status"] == "joined" else 1


if __name__ == "__main__":
    raise SystemExit(main())
