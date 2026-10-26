"""Split over-budget runner rows into sequential go-test chunks.

One runner TSV row becomes one ``go test -timeout=15m`` invocation, so a
row predicted past the budget cannot fit its own clock no matter how the
shard around it balances (#9133). This module cuts such package rows at
their weight midpoint until each chunk fits; the per-test report
accounting is unchanged because every chunk keeps canonical order and the
union of chunks is exactly the original row.
"""

from __future__ import annotations

import live_postgres_readiness_weights as wt

#: 80% of the 900s ``go test -timeout`` kill clock, matching the sibling
#: shard-job gate (1440s of 1800s). The observed red-vs-green swing is
#: +164s on a ~786s prediction, so a chunk predicted at 720s still clears
#: the clock (720 + 164 = 884 < 900); at 100% no room is left for growth.
ROW_BUDGET_TENTHS = 720 * wt.UNIT


def group_cost_tenths(group: list[str], weights: wt.Weights) -> int:
    """Predict tenths of a second to run this contiguous group as one row."""
    total = weights.cold
    previous = ""
    for line in group:
        package = line.split("|")[0]
        if package != previous:
            total += weights.package.get(package, weights.default_package)
            previous = package
        total += weights.test.get(line, weights.default_test)
    return total


def split_group(group: list[str], weights: wt.Weights) -> tuple[list[str], list[str]]:
    """Cut a group at its weight midpoint, keeping canonical order."""
    costs = [weights.test.get(line, weights.default_test) for line in group]
    half = sum(costs) / 2
    elapsed = 0
    for index, cost in enumerate(costs, start=1):
        if index >= len(group):
            break
        elapsed += cost
        if elapsed >= half:
            return group[:index], group[index:]
    middle = len(group) // 2
    return group[:middle], group[middle:]


def split_rows(groups: list[list[str]], weights: wt.Weights) -> list[list[str]]:
    """Split every over-budget group until each chunk fits its own clock.

    A single test heavier than the budget cannot be split and stays whole;
    the timeout then attributes correctly to that test instead of hiding
    inside a crowded row.
    """
    queue = list(groups)
    done: list[list[str]] = []
    while queue:
        group = queue.pop(0)
        if len(group) > 1 and group_cost_tenths(group, weights) > ROW_BUDGET_TENTHS:
            first, second = split_group(group, weights)
            queue.insert(0, second)
            queue.insert(0, first)
        else:
            done.append(group)
    return done
