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

#: 75% of the 900s ``go test -timeout`` kill clock (#9348). The v2 row price
#: is what that clock times (tests plus the repeat residual; compile and link
#: run before it starts). Over 17 green five-shard main runs, rows predicted
#: at 150 s or more ran at a measured/predicted ratio of p50 1.00, p99 1.26
#: and max 1.33 (n=188), so a 675 s row at the worst observed ratio runs
#: 675 x 1.33 = 898 s, still inside the clock.
ROW_BUDGET_TENTHS = 675 * wt.UNIT


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
