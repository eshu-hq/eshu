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
