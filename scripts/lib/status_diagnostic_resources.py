#!/usr/bin/env python3
"""Sample numeric process/cgroup CPU evidence at an actual one-second cadence."""

import argparse
import hashlib
import json
import os
import pathlib
import time


def process_ticks(text: str) -> int:
    """Return process user/system CPU ticks without retaining the command name."""
    fields = text.rsplit(")", 1)[-1].split()
    if len(fields) < 13:
        raise ValueError("missing process counters")
    values = [int(fields[11]), int(fields[12])]
    if min(values) < 0:
        raise ValueError("negative process counter")
    return sum(values)


def process_start_ticks(text: str) -> int:
    """Read Linux stat field22 starttime to pin the process instance."""
    fields = text.rsplit(")", 1)[-1].split()
    if len(fields) < 20 or int(fields[19]) <= 0:
        raise ValueError("missing process identity")
    return int(fields[19])


def cgroup_cpu(text: str) -> dict[str, int]:
    """Return only required numeric cgroup-v2 CPU totals."""
    keys = {"usage_usec", "user_usec", "system_usec"}
    result: dict[str, int] = {}
    for line in text.splitlines():
        fields = line.split()
        if len(fields) == 2 and fields[0] in keys:
            if fields[0] in result:
                raise ValueError("duplicate cgroup counter")
            result[fields[0]] = int(fields[1])
    if set(result) != keys or min(result.values()) < 0:
        raise ValueError("missing cgroup counters")
    return result


def _cgroup(pid: int) -> pathlib.Path:
    """Resolve the process's v2 cgroup, rejecting path escape and missing data."""
    lines = pathlib.Path(f"/proc/{pid}/cgroup").read_text(encoding="utf-8").splitlines()
    values = [line[3:] for line in lines if line.startswith("0::")]
    if len(values) != 1 or ".." in pathlib.PurePosixPath(values[0]).parts:
        raise ValueError("missing v2 cgroup")
    root = pathlib.Path("/sys/fs/cgroup")
    path = (root / values[0].lstrip("/")).resolve()
    if not path.is_relative_to(root):
        raise ValueError("invalid cgroup path")
    return path


def sample(pid: int, cgroup: pathlib.Path, expected_start: int) -> dict[str, object]:
    """Read numeric counters with wall and monotonic measurement boundaries."""
    started = time.monotonic_ns()
    stat = pathlib.Path(f"/proc/{pid}/stat").read_text(encoding="utf-8")
    if process_start_ticks(stat) != expected_start or _cgroup(pid) != cgroup:
        raise ValueError("process identity changed")
    cpu = cgroup_cpu((cgroup / "cpu.stat").read_text(encoding="utf-8"))
    memory = int((cgroup / "memory.current").read_text(encoding="utf-8"))
    if memory < 0:
        raise ValueError("invalid memory counter")
    elapsed = time.monotonic_ns() - started
    if elapsed < 0 or elapsed > 1000000000:
        raise ValueError("resource read exceeded sampling interval")
    return {"unix_nano": time.time_ns(), "monotonic_nano": started,
            "read_elapsed_nano": elapsed,
            "process_cpu_ticks": process_ticks(stat), "cgroup_cpu": cpu,
            "cgroup_memory_bytes": memory}


def main() -> int:
    """Capture bounded private evidence until the runner signals completion."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--api-pid", type=int, required=True)
    parser.add_argument("--postgres-pid", type=int, required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("--stop", type=pathlib.Path, required=True)
    parser.add_argument("--ready", type=pathlib.Path, required=True)
    args = parser.parse_args()
    try:
        processes = {"api": args.api_pid, "postgres_postmaster": args.postgres_pid}
        if min(processes.values()) <= 0:
            raise ValueError("invalid process")
        cgroups = {name: _cgroup(pid) for name, pid in processes.items()}
        identities = {name: process_start_ticks(pathlib.Path(f"/proc/{pid}/stat").read_text(encoding="utf-8"))
                      for name, pid in processes.items()}
        # This sampler reads only these processes; it never signals them.
        with os.fdopen(os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as output:
            metadata = {"schema_version": 1, "kind": "metadata", "cadence_seconds": 1,
                        "clock_ticks_per_second": os.sysconf("SC_CLK_TCK"),
                        "logical_cpus": os.cpu_count(), "process_start_ticks": identities,
                        "scope": "process ticks exclude children; cgroup totals may include other processes",
                        "cgroup_identity": {name: hashlib.sha256(str(path).encode()).hexdigest()
                                            for name, path in cgroups.items()}}
            output.write(json.dumps(metadata) + "\n")
            previous = time.monotonic_ns()
            count = 0
            late = False
            deadline = previous
            while not args.stop.exists() and count < 3600:
                now = time.monotonic_ns()
                gap = now - previous
                late = late or (count > 0 and gap > 2000000000)
                values = {name: sample(pid, cgroups[name], identities[name]) for name, pid in processes.items()}
                output.write(json.dumps({"kind": "sample", "gap_nano": gap, "processes": values}) + "\n")
                output.flush()
                if count == 0:
                    with os.fdopen(os.open(args.ready, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as ready:
                        ready.write("ready\n")
                previous = now
                count += 1
                deadline += 1000000000
                time.sleep(max(0, (deadline - time.monotonic_ns()) / 1e9))
            complete = count >= 2 and not late and count < 3600
            output.write(json.dumps({"kind": "completion", "complete": complete,
                                     "samples": count, "late_gap": late}) + "\n")
            return 0 if complete else 1
    except (OSError, ValueError):
        print("FAIL: status diagnostic resource evidence incomplete")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
