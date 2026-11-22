# Gate Timing

`make pre-push` and `make pre-pr` run many steps and gates, and the terminal
only shows how long each top-level step took. This page covers the timing record
each run writes, and the two commands that turn those records into a ranked list
of where the time goes.

The record is measurement only. It cannot change whether a step passes or
fails, and a failed write prints one warning line and the run carries on.

## What a run records

Each finished `make pre-push` or `make pre-pr` run appends one JSON line to
`~/.cache/eshu-gate-timing/runs.jsonl`. The file lives outside the repository on
purpose: it measures your host, so it is never committed. Every worktree on the
machine writes to the same file.

A line holds:

- the script, branch, short HEAD, base ref, CPU count, and wall seconds;
- the host load average at the start and end of the run, so a run taken on a
  busy host is easy to spot;
- each step's name, seconds, and exit code;
- each registry gate that actually ran, with its seconds, from the `ci-gates`
  report (skipped and reused commands cost nothing, so they are left out);
- each package's seconds from `go test` and `go test -race`;
- a small `meta` map, such as the number of changed Go packages.

At the end of a run the script prints the five slowest steps.

## Read the results

```bash
python3 scripts/dev/gate-timing.py summary                    # last 20 runs, both scripts
python3 scripts/dev/gate-timing.py summary --script pre-pr    # one script
python3 scripts/dev/gate-timing.py summary --last 50 --top 25
```

The summary shows the median and maximum seconds per step, per gate, and per
package. It counts runs whose end-of-run load average was above 9 as noisy,
because a timing taken on a busy host measures the host as much as the gate.
Compare runs of similar size: a run that changed 40 packages and a run that
changed one are different workloads.

For CI, the same view comes from the Actions API and needs the `gh` CLI signed
in with read access:

```bash
python3 scripts/dev/gate-timing.py ci                          # last 8 pull-request runs of "Build Test"
python3 scripts/dev/gate-timing.py ci --runs 20 --workflow "Build Test" --top 30
```

It reports the median queue wait, total job-minutes per run, and for each job
its median and maximum minutes. Shards of one job are grouped together.

## Settings

| Variable | Effect |
| --- | --- |
| `ESHU_GATE_TIMING=0` | Turns recording off for the run. |
| `ESHU_GATE_TIMING_DIR` | Directory for `runs.jsonl`. Default `~/.cache/eshu-gate-timing`. |

The tests under `scripts/` that run the real driver scripts in a scratch copy of
the repository set `ESHU_GATE_TIMING=0`, so test runs never enter your own
record.

## Limits

- The `make pre-pr` race lane runs its registry gates through a second
  `ci-gates` call that has no report file, so that lane shows as one step and not
  per gate.
- A run that exits before its steps start (for example, on an unresolvable base)
  records nothing.

The implementation is `scripts/dev/gate-timing.py` and
`scripts/lib/gate-timing.sh`, tested by `scripts/test-gate-timing.py`.
