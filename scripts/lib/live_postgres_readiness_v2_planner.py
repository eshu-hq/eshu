"""Exact guard-constrained lexicographic cuts for row-aware readiness costs."""

from __future__ import annotations

from collections.abc import Callable


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
