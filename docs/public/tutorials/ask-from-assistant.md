<!-- docs-catalog
title: Ask Eshu From An Assistant
description: Connects an MCP-capable assistant and asks one bounded evidence-backed Eshu question.
type: tutorial
audience: practitioner
time: 10 minutes
entrypoint: true
landing: false
-->

# Tutorial: Ask Eshu From An Assistant

Use this tutorial when you want Codex, Claude, Cursor, VS Code, or another MCP
client to ask Eshu bounded questions.

## Outcome

Your assistant can see Eshu MCP tools and answer a narrow question using indexed
repository evidence.

## Time

About 10 minutes after Eshu is running and has at least one indexed repository.

## Prerequisites

- A running local or hosted Eshu service.
- One repository indexed and ready.
- An MCP-capable assistant client.
- For hosted Eshu, `ESHU_SERVICE_URL` and `ESHU_API_KEY` set in your shell.

## Steps

1. Confirm the runtime is usable:

   ```bash
   eshu index-status
   eshu list
   ```

2. Generate a client snippet for your assistant:

   ```bash
   eshu mcp setup --platform codex
   ```

   Replace `codex` with `claude`, `cursor`, `vscode`, or `generic` as needed.

3. For hosted Eshu, include the hosted endpoint:

   ```bash
   eshu mcp setup --platform claude --hosted --service-url "$ESHU_SERVICE_URL"
   ```

4. Restart the assistant so it reloads the MCP server list.
5. Verify the setup with the same local or hosted shape you generated:

   ```bash
   eshu mcp setup --platform codex --verify
   eshu mcp setup --platform claude --hosted \
     --service-url "$ESHU_SERVICE_URL" --verify
   ```

   Use the first command for a local client snippet. Use the hosted command
   when the client should reach a deployed API/MCP endpoint with
   `ESHU_API_KEY` available in the assistant's environment.

6. Ask a narrow first prompt:

   ```text
   Use Eshu. List the indexed repositories, then explain what Eshu knows about
   one repository with file and symbol evidence.
   ```

## Choose A Tool Surface

Eshu can advertise its tools to an assistant in three shapes. The shape is
called the tool surface. It changes what the assistant sees in `tools/list`, not
what Eshu can answer.

| Surface | What the assistant sees | `tools/list` size on the validated build |
| --- | --- | --- |
| `full` (default) | Every tool, 163 on that build | about 210 KB |
| `router` | `find_tool` and `call_tool` only | about 1.5 KB |
| `core_router` | `find_tool`, `call_tool`, and 10 high-traffic tools | about 18 KB |

With `router` or `core_router`, the assistant asks `find_tool` for the tool that
fits its question, then runs it through `call_tool`. A tool the surface hides
still runs: `call_tool` reaches every other tool that the query profile and the
credential allow.

Pick the surface per connection by adding `?surface=` to the MCP URL. A hosted
client that POSTs to `<service-url>/mcp/message` uses
`<service-url>/mcp/message?surface=router`; an SSE client uses
`<service-url>/sse?surface=router`. Two assistants can use different surfaces
against the same Eshu. To change the default for everyone, set
`ESHU_MCP_TOOL_SURFACE` on the MCP server, or `mcpServer.env.ESHU_MCP_TOOL_SURFACE`
in the Helm chart. See [MCP Reference](../reference/mcp-reference.md#tool-surface).

The tool count that `eshu mcp setup --verify` prints is counted in the CLI
process for that build. A deployment that serves `router` or `core_router`
advertises fewer tools than the CLI reports.

What a hosted validation measured: one QA build, Claude Code and Codex with one
model each, 26 questions, three repeats per question and surface. Both clients
picked the right first tool most often on `router` (99% in Claude Code, 87% in
Codex). In Claude Code `router` used about one seventh of the input tokens of
`full`. `full` answered somewhat more questions in both clients, and
`core_router` was the weakest surface in both. The sample is small, so treat
gaps of a few runs as noise. The numbers, the scoring rules, and the gaps are in
the [validation note](https://github.com/eshu-hq/eshu/blob/main/docs/internal/evidence/8760-hosted-mcp-validation.md).

In practice: keep `full` when the assistant handles a long tool list well or you
want it to see every tool. Try `router` when tool-list size or input cost
matters and the assistant can follow a `find_tool` then `call_tool` sequence.

## Expected Result

The assistant can list Eshu tools and return an answer with evidence labels
instead of a guess. A successful setup proves more than endpoint reachability:
tools are visible and a first query can complete.

## Failure Hints

- If no tools appear, restart the assistant and confirm it reads the config
  path you edited.
- If hosted auth fails, export `ESHU_API_KEY` in the environment used to launch
  the assistant.
- If an answer is stale, run the stale-answer tutorial instead of retrying the
  prompt.
- If a prompt is broad, narrow it to a repository, file, symbol, service, or
  workload.

## Read Next

- [MCP Guide](../guides/mcp-guide.md) for tool selection and envelope details.
- [Connect MCP](../mcp/index.md) for setup options.
- [Starter Prompts](../guides/starter-prompts.md) for better first questions.
