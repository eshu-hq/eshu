"""Runs the #8760 task set against a hosted Eshu MCP server with a real agent client.

For each task and surface the runner starts the client as a subprocess in an
empty working directory, points it at ``<base>/mcp/message?surface=<surface>``,
records the event stream, and scores it with ``score``. Nothing about the
deployment is written to the repository: the base URL and the key come from the
environment, raw logs go to ``--out``, and entity bindings come from a private
``--bindings`` file.

Private data left behind: the ``--out`` raw logs hold tool results with real
entity names. Claude Code also stores large tool results it persists under
``~/.claude/projects/-tmp-eshu-eval-*``, outside ``--out``. Delete those
directories when the run is done. The runner removes its own temporary
directory at exit unless ``--keep`` is set.

Environment:
    ESHU_MCP_BASE_URL: Server base URL, for example ``https://host``.
    ESHU_MCP_KEY: Bearer key for the server. Never written to disk.

Example:
    ESHU_MCP_BASE_URL=https://host ESHU_MCP_KEY=... python3 run_eval.py \\
        --client claude --out /tmp/run1 --bindings /tmp/bindings.json

To compare result shapes with the same prompts, seed, and deployed build, run
once with ``--shape dual`` and once with ``--shape structured``. Give each run
a separate private ``--out`` directory. Omitting ``--shape`` keeps the server's
default URL behavior; that default is not assumed to be either explicit shape.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import json
import os
import pathlib
import random
import shutil
import subprocess
import sys
import tempfile
import time
from typing import Any

import score

SURFACES = score.SURFACES
_REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
_CORPUS = _REPO_ROOT / "go/internal/mcp/router/eval/testdata/corpus.json"
_TASKS = pathlib.Path(__file__).with_name("tasks.json")
_PROMPT = (
    "Answer the question using the Eshu MCP tools only. Do not guess: base the "
    "answer on tool results. Keep the answer to a few sentences.\n\nQuestion: {q}"
)


def load_tasks(bindings: dict[str, str]) -> list[tuple[score.Task, str]]:
    """Loads the task list with corpus expectations and question text.

    Args:
        bindings: Private overrides of question text, keyed by corpus id.

    Returns:
        Each task with the question text to send.

    Raises:
        SystemExit: If a listed id is not in the corpus.
    """
    corpus = {q["id"]: q for q in json.loads(_CORPUS.read_text())["questions"]}
    family_tools: dict[str, set[str]] = {}
    for q in corpus.values():
        family_tools.setdefault(q["family"], set()).update(
            [q["expected"], *(q.get("alternates") or ())])
    out = []
    for task_id in json.loads(_TASKS.read_text())["tasks"]:
        if task_id not in corpus:
            raise SystemExit(f"task {task_id} is not in the corpus")
        q = corpus[task_id]
        task = score.Task(task_id=task_id, family=q["family"],
                          expected=q["expected"],
                          alternates=tuple(q.get("alternates") or ()),
                          family_tools=frozenset(family_tools[q["family"]]))
        out.append((task, bindings.get(task_id, q["question"])))
    return out


def mcp_url(base_url: str, surface: str, shape: str = "") -> str:
    """Builds the shared hosted MCP URL for both clients."""
    url = f"{base_url.rstrip('/')}/mcp/message?surface={surface}"
    return f"{url}&shape={shape}" if shape else url


def write_mcp_config(directory: pathlib.Path, base_url: str, surface: str,
                     shape: str = "") -> str:
    """Writes a Claude Code MCP config whose header reads the key from the env."""
    path = directory / f"mcp_{surface}.json"
    path.write_text(json.dumps({"mcpServers": {"eshu": {
        "type": "http",
        "url": mcp_url(base_url, surface, shape),
        "headers": {"Authorization": "Bearer ${ESHU_MCP_KEY}"},
    }}}))
    return str(path)


def run_claude(question: str, config: str, model: str, cwd: str,
               timeout: int) -> tuple[str, score.Run]:
    """Runs one question through Claude Code and returns the raw stream and run.

    The client is isolated from the host's settings, memory files, and slash
    commands so every surface sees the same prompt context. Built-in tools are
    disabled, so the Eshu tools are the only way to answer.
    """
    cmd = ["claude", "-p", _PROMPT.format(q=question), "--mcp-config", config,
           "--strict-mcp-config", "--tools", "", "--allowedTools", "mcp__eshu",
           "--output-format", "stream-json", "--verbose", "--max-turns", "15",
           "--model", model, "--setting-sources", "", "--disable-slash-commands",
           "--no-session-persistence"]
    # Raise the client's MCP output limit so a large result arrives inline.
    # Claude Code 2.1.309 still saved some large results to a file the
    # tool-less agent cannot read; the scorer counts those as overflows, so
    # they show up as lost answers instead of silently passing.
    env = dict(os.environ, CLAUDE_CODE_DISABLE_CLAUDE_MDS="1",
               MAX_MCP_OUTPUT_TOKENS="200000")
    try:
        done = subprocess.run(cmd, cwd=cwd, env=env, capture_output=True,
                              text=True, timeout=timeout, stdin=subprocess.DEVNULL,
                              check=False)
        stream = done.stdout
    except subprocess.TimeoutExpired as exc:
        stream = (exc.stdout or b"").decode() if isinstance(exc.stdout, bytes) \
            else (exc.stdout or "")
        run = score.parse_claude(stream)
        run.run_error = True
        return stream, run
    return stream, score.parse_claude(stream)


def make_codex_home(directory: pathlib.Path) -> str:
    """Builds an isolated CODEX_HOME so only the Eshu server is configured.

    The login file is linked, not copied, so no credential is written to a new
    place. The host model and reasoning effort are carried over so runs use the
    same model the user's Codex would.
    """
    home = directory / "codex_home"
    home.mkdir()
    src = pathlib.Path.home() / ".codex"
    if not (src / "config.toml").is_file():
        raise SystemExit(
            f"{src / 'config.toml'} is missing: run Codex once so the runner "
            "can carry over the model and reasoning effort")
    for name in ("auth.json",):
        if (src / name).exists():
            (home / name).symlink_to(src / name)
    keep = []
    for line in (src / "config.toml").read_text().splitlines():
        if line.startswith("["):
            break
        if line.split("=")[0].strip() in ("model", "model_reasoning_effort"):
            keep.append(line)
    (home / "config.toml").write_text("\n".join(keep) + "\n")
    return str(home)


def run_codex(question: str, url: str, codex_home: str, cwd: str,
              timeout: int) -> tuple[str, score.Run]:
    """Runs one question through Codex and returns the raw stream and run.

    Shell access is off and MCP calls are pre-approved for the Eshu server, so
    the Eshu tools are the only way to answer.
    """
    cmd = ["codex", "exec", "--json", "--skip-git-repo-check", "--ephemeral",
           "--sandbox", "read-only", "-C", cwd,
           "-c", f'mcp_servers.eshu.url="{url}"',
           "-c", 'mcp_servers.eshu.bearer_token_env_var="ESHU_MCP_KEY"',
           "-c", 'mcp_servers.eshu.default_tools_approval_mode="approve"',
           "-c", "features.shell_tool=false", _PROMPT.format(q=question)]
    env = dict(os.environ, CODEX_HOME=codex_home)
    started = time.monotonic()
    timed_out = False
    try:
        done = subprocess.run(cmd, cwd=cwd, env=env, capture_output=True,
                              text=True, timeout=timeout,
                              stdin=subprocess.DEVNULL, check=False)
        stream = done.stdout
    except subprocess.TimeoutExpired as exc:
        timed_out = True
        out = exc.stdout
        stream = out.decode() if isinstance(out, bytes) else (out or "")
    run = score.parse_codex(stream)
    run.wall_seconds = time.monotonic() - started
    run.run_error = run.run_error or timed_out
    return stream, run


def codex_model(codex_home: str) -> str:
    """Returns the model line from the isolated Codex config, for the results."""
    for line in pathlib.Path(codex_home, "config.toml").read_text().splitlines():
        if line.startswith("model ") or line.startswith("model="):
            return line.split("=", 1)[1].strip().strip('"')
    return "default"


def client_version(client: str) -> str:
    """Returns the client's reported version for the evidence note."""
    done = subprocess.run([client, "--version"], capture_output=True, text=True,
                          check=False)
    return done.stdout.strip().splitlines()[0] if done.stdout else "unknown"


def check_bindings_path(path: pathlib.Path) -> None:
    """Refuses a bindings file inside the git repository.

    The file holds private question text, so it must never sit where
    ``git add`` could pick it up.

    Raises:
        SystemExit: If the resolved path is under the repository root.
    """
    resolved = path.resolve()
    if resolved == _REPO_ROOT or _REPO_ROOT in resolved.parents:
        raise SystemExit(
            f"--bindings {path} is inside the repository; the file holds "
            "private question text, so keep it outside the checkout")


def existing_result_shape(path: pathlib.Path, client: str) -> str:
    """Reads a prior result's shape without trusting its file or metadata.

    Args:
        path: Existing result file that the run would overwrite.
        client: Client expected in that file's metadata.

    Returns:
        The recorded shape, or ``server_default`` for a legacy result.

    Raises:
        SystemExit: If the file cannot be read or its envelope is malformed.
    """
    try:
        previous = json.loads(path.read_text())
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise SystemExit(f"cannot read existing results file {path}") from exc
    if not isinstance(previous, dict):
        raise SystemExit(f"invalid results envelope in {path}")
    meta = previous.get("meta")
    if not isinstance(meta, dict) or meta.get("client") != client:
        raise SystemExit(f"invalid results metadata in {path}")
    if not isinstance(previous.get("results"), list):
        raise SystemExit(f"invalid results envelope in {path}")
    if not all(score.valid_result_row(row) for row in previous["results"]):
        raise SystemExit(f"invalid results row in {path}")
    shape = meta.get("shape", "server_default")
    if shape not in ("server_default", "dual", "structured"):
        raise SystemExit(f"invalid results shape in {path}")
    return shape


def main(argv: list[str]) -> int:
    """Parses flags, runs the matrix, writes raw logs and scores."""
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--client", choices=("claude", "codex"), default="claude")
    ap.add_argument("--out", required=True, type=pathlib.Path)
    ap.add_argument("--bindings", type=pathlib.Path)
    ap.add_argument("--surfaces", default=",".join(SURFACES))
    ap.add_argument("--shape", choices=("dual", "structured"), default="",
                    help="explicit MCP result shape; omitted uses server default")
    ap.add_argument("--only", default="", help="comma-separated task ids")
    ap.add_argument("--model", default="sonnet")
    ap.add_argument("--workers", type=int, default=4)
    ap.add_argument("--timeout", type=int, default=420)
    ap.add_argument("--seed", type=int, default=8760)
    ap.add_argument("--repeat", type=int, default=1,
                    help="runs per task and surface; model output varies")
    ap.add_argument("--keep", action="store_true",
                    help="keep the temporary directory (MCP configs, Codex home)")
    ap.add_argument("--build", default="",
                    help="deployed Eshu build SHA, recorded in the results")
    args = ap.parse_args(argv)
    result_path = args.out / f"results_{args.client}.json"
    if result_path.exists():
        previous_shape = existing_result_shape(result_path, args.client)
        requested_shape = args.shape or "server_default"
        if previous_shape != requested_shape:
            raise SystemExit(
                f"{result_path} contains shape {previous_shape}; requested "
                f"{requested_shape}. Use separate private --out directories")
    base = os.environ.get("ESHU_MCP_BASE_URL", "")
    if not base or not os.environ.get("ESHU_MCP_KEY"):
        raise SystemExit("set ESHU_MCP_BASE_URL and ESHU_MCP_KEY")
    if args.bindings:
        check_bindings_path(args.bindings)
    bindings = json.loads(args.bindings.read_text()) if args.bindings else {}
    only = {t for t in args.only.split(",") if t}
    tasks = [t for t in load_tasks(bindings) if not only or t[0].task_id in only]
    surfaces = [s for s in args.surfaces.split(",") if s]
    jobs = [(t, q, s, i) for t, q in tasks for s in surfaces
            for i in range(args.repeat)]
    random.Random(args.seed).shuffle(jobs)
    tmp = pathlib.Path(tempfile.mkdtemp(prefix="eshu-eval-"))
    try:
        configs = {s: write_mcp_config(tmp, base, s, args.shape) for s in surfaces}
        urls = {s: mcp_url(base, s, args.shape) for s in surfaces}
        codex_home = make_codex_home(tmp) if args.client == "codex" else ""
        cwd = str(tmp / "cwd"); os.makedirs(cwd)
        raw = args.out / "raw" / args.client
        raw.mkdir(parents=True, exist_ok=True)

        def one(job: tuple[score.Task, str, str, int]) -> dict[str, Any]:
            task, question, surface, repeat = job
            if args.client == "codex":
                stream, run = run_codex(question, urls[surface], codex_home, cwd,
                                        args.timeout)
            else:
                stream, run = run_claude(question, configs[surface], args.model,
                                         cwd, args.timeout)
            (raw / f"{surface}__{task.task_id}__{repeat}.jsonl").write_text(stream)
            sc = score.score(task, run)
            return {"surface": surface, "repeat": repeat,
                    "non_mcp_calls": run.non_mcp_calls, "turns": run.turns,
                    **sc.__dict__}

        results = []
        with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
            for r in pool.map(one, jobs):
                results.append(r)
                print(f"{r['surface']:<12} {r['task_id']:<26} #{r['repeat']} "
                      f"calls={r['tool_calls']} "
                      f"wrong={r['wrong_tool_calls']} ok={r['success']} "
                      f"{r['wall_seconds']:.0f}s", flush=True)
        model = codex_model(codex_home) if args.client == "codex" else args.model
        meta = {"client": args.client, "version": client_version(args.client),
                "model": model, "seed": args.seed, "repeat": args.repeat,
                "build": args.build, "shape": args.shape or "server_default"}
        result_path.write_text(
            json.dumps({"meta": meta, "results": results}, indent=1))
        return 0
    finally:
        if not args.keep:
            shutil.rmtree(tmp, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
