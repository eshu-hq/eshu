"""Exact guard-constrained lexicographic cuts for row-aware readiness costs."""

from __future__ import annotations

from collections.abc import Callable

_SPREAD_PERCENT = 15
_SPREAD_SEARCH_BUDGET = 10_000_000


def exact_bounds(count: int, shards: int, cap: int,
                 cost: Callable[[int, int], int]) -> list[int]:
    """Maximize minimum, minimize maximum, then minimize squared job costs.

    Every interval is inspected because midpoint row counts make cost
    nonmonotone in interval length. Ties keep the first ascending cut.
    """
    if count < shards or shards < 1 or cap < 0:
        raise ValueError("invalid exact partition dimensions")
    infinity = 1 << 62
    minima = [[-1] * (count + 1) for _ in range(shards + 1)]
    minima[0][0] = infinity
    for parts in range(1, shards + 1):
        for end in range(parts, count - (shards - parts) + 1):
            for begin in range(parts - 1, end):
                prior = minima[parts - 1][begin]
                if prior < 0:
                    continue
                segment = cost(begin, end)
                if segment <= cap:
                    minima[parts][end] = max(minima[parts][end], min(prior, segment))
    lower = minima[shards][count]
    if lower < 0:
        raise ValueError("no shard partition fits the job budget")

    maxima = [[infinity] * (count + 1) for _ in range(shards + 1)]
    maxima[0][0] = 0
    for parts in range(1, shards + 1):
        for end in range(parts, count - (shards - parts) + 1):
            for begin in range(parts - 1, end):
                if maxima[parts - 1][begin] == infinity:
                    continue
                segment = cost(begin, end)
                if lower <= segment <= cap:
                    maxima[parts][end] = min(maxima[parts][end],
                                             max(maxima[parts - 1][begin], segment))
    upper = maxima[shards][count]
    if upper > cap:
        raise ValueError("no partition reaches the optimal minimum")

    squares = [[infinity] * (count + 1) for _ in range(shards + 1)]
    choices = [[-1] * (count + 1) for _ in range(shards + 1)]
    squares[0][0] = 0
    for parts in range(1, shards + 1):
        for end in range(parts, count - (shards - parts) + 1):
            for begin in range(parts - 1, end):
                prior = squares[parts - 1][begin]
                if prior == infinity:
                    continue
                segment = cost(begin, end)
                if lower <= segment <= upper:
                    candidate = prior + segment * segment
                    if candidate < squares[parts][end]:
                        squares[parts][end] = candidate
                        choices[parts][end] = begin
    if choices[shards][count] < 0:
        raise ValueError("no partition reaches the optimal extrema")
    bounds = [count]
    for parts in range(shards, 0, -1):
        bounds.append(choices[parts][bounds[-1]])
    bounds.reverse()
    if bounds[0] != 0 or any(a >= b for a, b in zip(bounds, bounds[1:])):
        raise ValueError("exact partition produced an empty shard")
    return bounds


class _SpreadBudgetExhausted(Exception):
    """Internal signal: the bounded spread search gave up fail-closed."""


def _spread_ok(values: list[int], shards: int) -> bool:
    """Check the integer spread budget exactly as the weights gate does.

    A cut passes when every job stays within 15% of the mean, computed
    in integers as ``max(abs(value * shards - total)) * 100`` against
    ``15 * total``. An all-zero cut passes.
    """
    total = sum(values)
    if total == 0:
        return True
    return (max(abs(value * shards - total) for value in values) * 100
            <= _SPREAD_PERCENT * total)


def exact_bounds_spread(count: int, shards: int, cap: int,
                        cost: Callable[[int, int], int]) -> list[int]:
    """Cut into shards meeting the cap at the same lexicographic optimum.

    Among cap-feasible cuts whose jobs meet the 15% spread budget, this
    maximizes the minimum, minimizes the maximum, then minimizes squared
    job costs. Ties keep the first ascending cut, matching exact_bounds.

    The search seeds from exact_bounds, prunes cap-violating and
    lexicographically dominated branches, and compares full keys
    including the bounds so ties resolve identically. It evaluates at
    most _SPREAD_SEARCH_BUDGET segments and fails closed with
    ValueError when the budget is exhausted or no cut meets the spread.

    Args:
        count: The number of items to partition.
        shards: The number of nonempty contiguous shards.
        cap: The maximum cost of any single shard.
        cost: The cost of the half-open interval [low, high).

    Returns:
        The shard_count + 1 ascending boundaries starting at 0.

    Raises:
        ValueError: The dimensions are invalid, no cap-feasible cut
            exists, or no cap-feasible cut meets the spread budget.
    """
    if count < shards or shards < 1 or cap < 0:
        raise ValueError("invalid exact partition dimensions")
    seed = exact_bounds(count, shards, cap, cost)
    seed_values = [cost(low, high) for low, high in zip(seed, seed[1:])]
    best_key: tuple[int, int, int, tuple[int, ...]] | None = None
    if _spread_ok(seed_values, shards):
        best_key = (-min(seed_values), max(seed_values),
                    sum(value * value for value in seed_values),
                    tuple(seed))
    spent = 0
    cuts = [0]
    segments: list[int] = []

    def search(start: int, done: int, least: int, most: int,
               squares: int) -> None:
        """Extend the cut depth-first, tracking the running key prefix."""
        nonlocal best_key, spent
        if done == shards - 1:
            spent += 1
            if spent > _SPREAD_SEARCH_BUDGET:
                raise _SpreadBudgetExhausted
            segment = cost(start, count)
            if segment > cap:
                return
            values = segments + [segment]
            if not _spread_ok(values, shards):
                return
            key = (-min(values), max(values),
                   sum(value * value for value in values),
                   tuple(cuts + [count]))
            if best_key is None or key < best_key:
                best_key = key
            return
        for cut in range(start + 1, count - shards + done + 2):
            spent += 1
            if spent > _SPREAD_SEARCH_BUDGET:
                raise _SpreadBudgetExhausted
            segment = cost(start, cut)
            if segment > cap:
                continue
            if done:
                branch_least = min(least, segment)
                branch_most = max(most, segment)
                branch_squares = squares + segment * segment
            else:
                branch_least, branch_most = segment, segment
                branch_squares = segment * segment
            if best_key is not None:
                floor, ceiling, quota = best_key[0], best_key[1], best_key[2]
                if (branch_least < -floor
                        or (branch_least == -floor
                            and (branch_most > ceiling
                                 or (branch_most == ceiling
                                     and branch_squares > quota)))):
                    continue
            cuts.append(cut)
            segments.append(segment)
            search(cut, done + 1, branch_least, branch_most, branch_squares)
            cuts.pop()
            segments.pop()

    try:
        search(0, 0, 0, 0, 0)
    except _SpreadBudgetExhausted:
        raise ValueError("no shard partition meets the spread budget")
    if best_key is None:
        raise ValueError("no shard partition meets the spread budget")
    return list(best_key[3])
