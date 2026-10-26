"""Scoring for the hosted MCP surface evaluation (issue #8760).

This module turns the event stream of one agent run into the four per-task
measures the issue asks for: tool calls, wrong-tool calls, task success, and
wall time. It has no network or client dependency so the rules can be tested
and read on their own.

The rules, stated once:

* A call is an MCP call to the Eshu server. Calls to other tools (a shell,
  say) are counted separately and never score as Eshu calls.
* ``call_tool`` is a wrapper. The wrapped tool name is the call's target.
  ``find_tool`` is a discovery hop: it counts as a tool call but is not a
  domain call, so it can never be a wrong tool.
* A domain call is wrong when its target is neither the corpus ``expected``
  tool nor one of its ``alternates``.
* A wrong call is split three ways. A *helper* call is a lookup that finds
  identifiers or context for the real tool (``HELPERS``). A *sibling* call is a
  tool from the same corpus family that the corpus does not list for the
  question. An *off-family* call is anything else. Only sibling and off-family
  calls are counted as genuinely wrong.
* A result the client could not read (it overflowed the output limit and was
  saved to a file) is not an answer and never counts toward success.
* A task succeeds when an expected or alternate tool returned a readable result
  without an error and the agent produced a non-empty final answer. This is a
  proxy for the right tool answering. It does not grade the text of the answer,
  and a question the deployment has no data for can still succeed.
"""

from __future__ import annotations

import dataclasses
import json
import math
import statistics
from typing import Any, Iterable

_SERVER_PREFIX = "mcp__eshu__"
_WRAPPER = "call_tool"
_DISCOVERY = "find_tool"
# Wording the client uses when it cannot show a tool result: the older error
# text, and the newer notice that swaps the result for a short preview and a
# file path the tool-less agent cannot open. The error text may follow a prefix,
# so it matches anywhere. The saved-file notice is the whole result, so it must
# be at the start: a readable result that quotes the marker is not an overflow.
_OVERFLOW_ERROR = "exceeds maximum allowed tokens"
_SAVED_NOTICE = "<persisted-output>"

# Lookups that resolve identifiers or give context for another tool. A call to
# one of these is not counted as a wrong pick even when the corpus does not list
# it for the question.
HELPERS = frozenset({
    "list_indexed_repositories", "resolve_entity", "find_symbol", "find_code",
    "get_repo_summary", "get_repo_context", "get_repo_story",
    "get_repository_stats", "list_repository_files",
})

SURFACES = ("full", "router", "core_router")


@dataclasses.dataclass(frozen=True)
class Task:
    """One evaluation task taken from the router eval corpus.

    Attributes:
        task_id: Corpus question id, such as ``code-lookup-001``.
        family: Corpus family label.
        expected: The tool that best answers the question.
        alternates: Other tools that also answer it acceptably.
        family_tools: Every tool the corpus lists as expected or alternate for
            any question in the same corpus family.
    """

    task_id: str
    family: str
    expected: str
    alternates: tuple[str, ...] = ()
    family_tools: frozenset[str] = frozenset()

    @property
    def acceptable(self) -> frozenset[str]:
        """Returns every tool name that counts as a correct pick."""
        return frozenset((self.expected, *self.alternates))


@dataclasses.dataclass
class Call:
    """One MCP call made by the agent.

    Attributes:
        name: Tool name as the client invoked it (``call_tool`` for wrapped).
        target: The tool that did the work; the wrapped name for ``call_tool``.
        is_error: Whether the tool result was an error.
        overflowed: Whether the client could not read the result because it
            exceeded the output limit.
    """

    name: str
    target: str
    is_error: bool = False
    overflowed: bool = False


@dataclasses.dataclass
class Run:
    """The parsed outcome of one agent run.

    Attributes:
        calls: Eshu MCP calls in the order they were made.
        final_text: The agent's final answer text.
        wall_seconds: Wall time the client reported for the run.
        turns: Number of agent turns the client reported.
        run_error: Whether the client itself reported a failed run.
        non_mcp_calls: Calls to tools that are not the Eshu server.
        cost_usd: Cost the client reported for the run.
        input_tokens: Input-side tokens: fresh, cache-written and cache-read.
        output_tokens: Output tokens.
    """

    calls: list[Call]
    final_text: str
    wall_seconds: float
    turns: int
    run_error: bool
    non_mcp_calls: int = 0
    cost_usd: float = 0.0
    input_tokens: int = 0
    output_tokens: int = 0


@dataclasses.dataclass(frozen=True)
class Score:
    """The four per-task measures, plus the fields that explain them."""

    task_id: str
    family: str
    tool_calls: int
    domain_calls: int
    wrong_tool_calls: int
    first_domain_call_correct: bool
    success: bool
    wall_seconds: float
    cost_usd: float = 0.0
    input_tokens: int = 0
    output_tokens: int = 0
    helper_calls: int = 0
    sibling_wrong_calls: int = 0
    off_family_wrong_calls: int = 0
    overflowed_calls: int = 0


def _finite_nonnegative(value: object) -> bool:
    """Checks a JSON number used in score arithmetic, excluding booleans."""
    if type(value) not in (int, float):
        return False
    try:
        return value >= 0 and math.isfinite(value)
    except OverflowError:
        return False


def valid_result_row(row: object) -> bool:
    """Checks fields consumed when a stored result is grouped and scored.

    Optional Score fields may be absent in older files. Extra fields are
    ignored so newer runners remain readable by this report.

    Args:
        row: Decoded JSON value from a result file's ``results`` list.

    Returns:
        Whether the fields needed by both consumers have usable types.
    """
    if not isinstance(row, dict) or row.get("surface") not in SURFACES:
        return False
    for key in ("task_id", "family"):
        value = row.get(key)
        if not isinstance(value, str) or not value.strip():
            return False
    for key in ("tool_calls", "domain_calls", "wrong_tool_calls"):
        value = row.get(key)
        if type(value) is not int or value < 0:
            return False
    for key in ("helper_calls", "sibling_wrong_calls",
                "off_family_wrong_calls", "overflowed_calls",
                "input_tokens", "output_tokens"):
        if key in row and (type(row[key]) is not int or row[key] < 0):
            return False
    for key in ("first_domain_call_correct", "success"):
        if type(row.get(key)) is not bool:
            return False
    if not _finite_nonnegative(row.get("wall_seconds")):
        return False
    if "cost_usd" in row and not _finite_nonnegative(row["cost_usd"]):
        return False
    return True


def parse_claude(stream: str) -> Run:
    """Parses Claude Code ``--output-format stream-json`` output.

    Args:
        stream: The JSON-lines text the CLI wrote. Lines that are not JSON are
            skipped.

    Returns:
        The ordered Eshu calls and the run outcome.
    """
    calls: list[Call] = []
    by_id: dict[str, Call] = {}
    final_text, wall, turns, run_error, non_mcp = "", 0.0, 0, False, 0
    cost, tokens_in, tokens_out = 0.0, 0, 0
    for line in stream.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if not isinstance(event, dict):
            continue
        kind = event.get("type")
        content = (event.get("message") or {}).get("content") or []
        if kind == "assistant":
            for block in content:
                if block.get("type") != "tool_use":
                    continue
                name = str(block.get("name", ""))
                if not name.startswith(_SERVER_PREFIX):
                    non_mcp += 1
                    continue
                call = _call_from(name[len(_SERVER_PREFIX):], block.get("input"))
                calls.append(call)
                by_id[str(block.get("id"))] = call
        elif kind == "user":
            for block in content:
                if block.get("type") == "tool_result":
                    call = by_id.get(str(block.get("tool_use_id")))
                    if call is not None:
                        call.is_error = bool(block.get("is_error"))
                        text = _text_of(block.get("content"))
                        call.overflowed = (
                            _OVERFLOW_ERROR in text
                            or text.strip().startswith(_SAVED_NOTICE))
        elif kind == "result":
            final_text = str(event.get("result") or "")
            wall = float(event.get("duration_ms") or 0) / 1000.0
            turns = int(event.get("num_turns") or 0)
            run_error = bool(event.get("is_error"))
            cost = float(event.get("total_cost_usd") or 0)
            usage = event.get("usage") or {}
            tokens_in = sum(int(usage.get(k) or 0) for k in (
                "input_tokens", "cache_creation_input_tokens",
                "cache_read_input_tokens"))
            tokens_out = int(usage.get("output_tokens") or 0)
    return Run(calls=calls, final_text=final_text, wall_seconds=wall,
               turns=turns, run_error=run_error, non_mcp_calls=non_mcp,
               cost_usd=cost, input_tokens=tokens_in, output_tokens=tokens_out)


def parse_codex(stream: str) -> Run:
    """Parses Codex ``exec --json`` output.

    Codex reports completed items and a final usage record but no wall time, so
    ``wall_seconds`` is left at zero for the runner to fill from its own clock.
    ``input_tokens`` is Codex's reported total, which already includes cached
    tokens.

    Args:
        stream: The JSON-lines text the CLI wrote. Lines that are not JSON are
            skipped.

    Returns:
        The ordered Eshu calls and the run outcome.
    """
    calls: list[Call] = []
    final_text, run_error, non_mcp = "", False, 0
    tokens_in, tokens_out = 0, 0
    for line in stream.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if not isinstance(event, dict):
            continue
        kind = event.get("type")
        item = event.get("item") or {}
        if kind == "item.completed":
            item_kind = item.get("type")
            if item_kind == "mcp_tool_call":
                if item.get("server") != "eshu":
                    non_mcp += 1
                    continue
                call = _call_from(str(item.get("tool", "")), item.get("arguments"))
                call.is_error = item.get("status") != "completed" or bool(
                    item.get("error"))
                calls.append(call)
            elif item_kind == "command_execution":
                non_mcp += 1
            elif item_kind == "agent_message":
                final_text = str(item.get("text") or "")
        elif kind == "turn.completed":
            usage = event.get("usage") or {}
            tokens_in = int(usage.get("input_tokens") or 0)
            tokens_out = int(usage.get("output_tokens") or 0)
        elif kind == "turn.failed":
            run_error = True
    return Run(calls=calls, final_text=final_text, wall_seconds=0.0, turns=0,
               run_error=run_error, non_mcp_calls=non_mcp,
               input_tokens=tokens_in, output_tokens=tokens_out)


def _text_of(content: Any) -> str:
    """Flattens a tool_result content value (string or blocks) to text."""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return " ".join(str(b.get("text", "")) if isinstance(b, dict) else str(b)
                        for b in content)
    return ""


def _call_from(name: str, tool_input: Any) -> Call:
    """Builds a Call, unwrapping ``call_tool`` to the tool it dispatched."""
    target = name
    if name == _WRAPPER and isinstance(tool_input, dict):
        target = str(tool_input.get("name") or name)
    return Call(name=name, target=target)


def score(task: Task, run: Run) -> Score:
    """Applies the issue's per-task measures to one run.

    Args:
        task: The task the run answered.
        run: The parsed run.

    Returns:
        The task's score.
    """
    domain = [c for c in run.calls if c.target != _DISCOVERY]
    wrong = [c for c in domain if c.target not in task.acceptable]
    helper = [c for c in wrong if c.target in HELPERS]
    rest = [c for c in wrong if c.target not in HELPERS]
    sibling = [c for c in rest if c.target in task.family_tools]
    first_ok = bool(domain) and domain[0].target in task.acceptable
    answered = any(c.target in task.acceptable and not c.is_error
                   and not c.overflowed for c in domain)
    success = answered and bool(run.final_text.strip()) and not run.run_error
    return Score(task_id=task.task_id, family=task.family,
                 tool_calls=len(run.calls), domain_calls=len(domain),
                 wrong_tool_calls=len(wrong), first_domain_call_correct=first_ok,
                 success=success, wall_seconds=run.wall_seconds,
                 cost_usd=run.cost_usd, input_tokens=run.input_tokens,
                 output_tokens=run.output_tokens, helper_calls=len(helper),
                 sibling_wrong_calls=len(sibling),
                 off_family_wrong_calls=len(rest) - len(sibling),
                 overflowed_calls=sum(1 for c in run.calls if c.overflowed))


def summarize(scores: Iterable[Score]) -> dict[str, float]:
    """Rolls task scores up for one client and surface.

    Rates use domain calls (wrong-tool rate) and tasks (the others), so a
    surface cannot look better by making fewer, wrong calls.

    Args:
        scores: The task scores for one client and surface.

    Returns:
        The aggregate measures; empty input returns zeros.
    """
    rows = list(scores)
    n = len(rows)
    domain = sum(r.domain_calls for r in rows)
    return {
        "tasks": n,
        "mean_tool_calls": (sum(r.tool_calls for r in rows) / n) if n else 0.0,
        "wrong_tool_rate": (sum(r.wrong_tool_calls for r in rows) / domain)
        if domain else 0.0,
        "success_rate": (sum(r.success for r in rows) / n) if n else 0.0,
        "first_try_rate": (sum(r.first_domain_call_correct for r in rows) / n)
        if n else 0.0,
        "median_wall_seconds": statistics.median(
            r.wall_seconds for r in rows) if n else 0.0,
        "helper_rate": (sum(r.helper_calls for r in rows) / domain)
        if domain else 0.0,
        "genuine_wrong_rate": (sum(r.sibling_wrong_calls
                                   + r.off_family_wrong_calls for r in rows)
                               / domain) if domain else 0.0,
        "overflowed_calls": sum(r.overflowed_calls for r in rows),
        "mean_cost_usd": (sum(r.cost_usd for r in rows) / n) if n else 0.0,
        "mean_input_tokens": (sum(r.input_tokens for r in rows) / n)
        if n else 0.0,
        "mean_output_tokens": (sum(r.output_tokens for r in rows) / n)
        if n else 0.0,
    }
