"""Compare two parity captures for #9990 and print the moved-row counts."""

import collections
import json
import sys
from typing import Dict, Set, Tuple

Key = Tuple[str, str, str]
Result = Tuple[int, bool, str]


def load(path: str) -> Dict[Key, Set[Result]]:
    """Reads one capture into its results per (kind, shape, input digest)."""
    rows: Dict[Key, Set[Result]] = {}
    with open(path, encoding="utf-8") as capture:
        for line in capture:
            row = json.loads(line)
            key = (row["kind"], row["shape"], row["in"])
            rows.setdefault(key, set()).add((row["n"], row["err"], row["out"]))
    return rows


def classify(base: Set[Result], head: Set[Result]) -> str:
    """Names how one call moved from the base capture to the head capture."""
    if base == head:
        return "identical"
    base_rows = max(result[0] for result in base)
    head_rows = max(result[0] for result in head)
    if base_rows == 0:
        return "new_row_where_base_had_none"
    return "lost_a_row" if head_rows < base_rows else "row_changed"


def order_dependent(rows: Dict[Key, Set[Result]]) -> int:
    """Counts grid shapes whose two fact orders gave different findings."""
    shapes: Dict[str, Dict[str, list]] = collections.defaultdict(dict)
    for (kind, shape, _), results in rows.items():
        if kind == "grid":
            label, order = shape.rsplit("|order=", 1)
            shapes[label][order] = sorted(results)
    return sum(1 for orders in shapes.values() if orders.get("0") != orders.get("1"))


def main(base_path: str, head_path: str) -> None:
    """Prints the differential that the evidence note records."""
    base, head = load(base_path), load(head_path)
    common = set(base) & set(head)
    print(f"base-only keys {len(set(base) - common)}, head-only keys {len(set(head) - common)}")
    counts: collections.Counter = collections.Counter()
    language_changed = 0
    for key in common:
        kind, shape, _ = key
        counts[(kind, classify(base[key], head[key]))] += 1
        if shape.startswith("lang|") and base[key] != head[key]:
            language_changed += 1
    for (kind, outcome), count in sorted(counts.items()):
        print(f"{kind} {outcome} {count}")
    print(f"language calls changed {language_changed}")
    print(f"order-dependent shapes: base {order_dependent(base)}, head {order_dependent(head)}")


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
