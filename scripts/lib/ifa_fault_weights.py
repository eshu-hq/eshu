#!/usr/bin/env python3
"""Generate the Ifá fault-injection shard weights from measured cell wall times.

The shard partitioner (scripts/lib/ifa_fault_shard.sh) balances shards on the
seconds each cell costs, and the shard-balance mirror turns an over-budget shard
into a red check instead of a cancelled CI job. Those seconds used to be hand
copied from one CI backend; this tool derives them from recorded measurements so
a backend change cannot leave a stale model behind.

Inputs and outputs:

* scripts/lib/ifa_fault_neo4j_walls.tsv holds one measured wall time per line:
  ``cell<TAB>seconds<TAB>source``. Several lines for one cell are reduced to
  their median. The ``cell_baseline`` rows (the baseline every shard repeats)
  feed IFA_FAULT_SHARD_BASELINE_WEIGHT_SECONDS.
* The weight block between the BEGIN/END markers in
  scripts/lib/ifa_fault_shard_cells.sh is generated. The default weight, which a
  cell with no measurement costs, is the median of the per-cell medians.

Sub-commands:

  table    print the generated block for the TSV on stdout
  write    rewrite the block in the cells file in place
  check    exit 1 when the block in the cells file differs from the TSV
  collect  print TSV rows for one shard log: pair the log's ``cell wall time``
           lines with the shard's ordered cell list (``--list-cells --shard k/n``
           output), in dispatch order, and refuse a count mismatch
"""

import argparse
import pathlib
import re
import statistics
import sys

BEGIN = "# BEGIN GENERATED WEIGHTS (scripts/lib/ifa_fault_weights.py)"
END = "# END GENERATED WEIGHTS"
BASELINE_CELL = "cell_baseline"
_WALL_RE = re.compile(r"cell wall time: (\d+)s")
_ROW_RE = re.compile(r"^(cell_[a-z0-9_]+)\t(\d+)\t(\S+)$")


def _round_half_up(value: float) -> int:
    """Rounds a non-negative median to whole seconds, halves up."""
    return int(value + 0.5)


def read_walls(path: pathlib.Path) -> dict[str, list[int]]:
    """Reads the measurement TSV into cell -> list of wall seconds.

    Args:
      path: The TSV file. ``#`` lines and blank lines are skipped.

    Returns:
      A mapping from cell name to every recorded wall time.

    Raises:
      ValueError: A line is not ``cell<TAB>seconds<TAB>source`` or a time is 0.
    """
    walls: dict[str, list[int]] = {}
    for number, line in enumerate(path.read_text().splitlines(), start=1):
        if not line.strip() or line.startswith("#"):
            continue
        match = _ROW_RE.match(line)
        if match is None or int(match.group(2)) < 1:
            raise ValueError(f"{path}:{number}: want cell<TAB>seconds<TAB>source, got {line!r}")
        walls.setdefault(match.group(1), []).append(int(match.group(2)))
    if not walls:
        raise ValueError(f"{path}: no measurements")
    return walls


def derive(walls: dict[str, list[int]]) -> tuple[int, int, dict[str, int]]:
    """Reduces measurements to the default, baseline and per-cell weights.

    Args:
      walls: Cell -> recorded wall times, from read_walls.

    Returns:
      (default weight, baseline weight, per-cell weights without the baseline).

    Raises:
      ValueError: There is no baseline row or no non-baseline row.
    """
    medians = {cell: _round_half_up(statistics.median(times)) for cell, times in walls.items()}
    if BASELINE_CELL not in medians:
        raise ValueError(f"no {BASELINE_CELL} measurement: every shard repeats it")
    baseline = medians.pop(BASELINE_CELL)
    if not medians:
        raise ValueError("no non-baseline measurement")
    default = _round_half_up(statistics.median(medians.values()))
    return default, baseline, dict(sorted(medians.items()))


def render(default: int, baseline: int, cells: dict[str, int]) -> str:
    """Renders the generated block, markers included, newline terminated."""
    lines = [
        BEGIN,
        "# Do not edit: change scripts/lib/ifa_fault_neo4j_walls.tsv and run",
        "#   python3 scripts/lib/ifa_fault_weights.py write",
        f"IFA_FAULT_SHARD_BASELINE_WEIGHT_SECONDS={baseline}",
        f"IFA_FAULT_CELL_DEFAULT_WEIGHT_SECONDS={default}",
        "declare -gA IFA_FAULT_CELL_WEIGHT_SECONDS=(",
    ]
    lines += [f"\t[{cell}]={seconds}" for cell, seconds in cells.items()]
    lines += [")", END, ""]
    return "\n".join(lines)


def _split(text: str) -> tuple[str, str, str]:
    """Splits the cells file into (before, block, after) around the markers."""
    start = text.find(BEGIN)
    stop = text.find(END)
    if start < 0 or stop < start:
        raise ValueError("generated-weights markers are missing or out of order")
    stop = text.index("\n", stop) + 1
    return text[:start], text[start:stop], text[stop:]


def collect(log: pathlib.Path, cells: pathlib.Path, source: str) -> str:
    """Pairs a shard log's wall-time lines with its ordered cell list."""
    names = [line.strip() for line in cells.read_text().splitlines() if line.strip()]
    times = [int(t) for t in _WALL_RE.findall(log.read_text())]
    if len(names) != len(times):
        raise ValueError(f"{len(names)} cells but {len(times)} 'cell wall time' lines in {log}")
    return "".join(f"{name}\t{seconds}\t{source}\n" for name, seconds in zip(names, times))


def main(argv: list[str]) -> int:
    """Runs the sub-command named on the command line."""
    root = pathlib.Path(__file__).resolve().parent
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawTextHelpFormatter)
    parser.add_argument("command", choices=["table", "write", "check", "collect"])
    parser.add_argument("--walls", type=pathlib.Path, default=root / "ifa_fault_neo4j_walls.tsv")
    parser.add_argument("--cells-file", type=pathlib.Path, default=root / "ifa_fault_shard_cells.sh")
    parser.add_argument("--log", type=pathlib.Path, help="collect: shard log with 'cell wall time' lines")
    parser.add_argument("--cell-list", type=pathlib.Path, help="collect: ordered cell names, one per line")
    parser.add_argument("--source", default="unlabeled", help="collect: label stored in the source column")
    args = parser.parse_args(argv)
    try:
        if args.command == "collect":
            if args.log is None or args.cell_list is None:
                parser.error("collect needs --log and --cell-list")
            sys.stdout.write(collect(args.log, args.cell_list, args.source))
            return 0
        block = render(*derive(read_walls(args.walls)))
        if args.command == "table":
            sys.stdout.write(block)
            return 0
        text = args.cells_file.read_text()
        before, current, after = _split(text)
        if args.command == "check":
            if current != block:
                print(f"ifa_fault_weights: the generated block in {args.cells_file} is stale; "
                      f"run: python3 scripts/lib/ifa_fault_weights.py write", file=sys.stderr)
                return 1
            return 0
        args.cells_file.write_text(before + block + after)
        return 0
    except (OSError, ValueError) as err:
        print(f"ifa_fault_weights: {err}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
