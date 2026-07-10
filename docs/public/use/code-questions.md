<!-- docs-catalog
title: Ask Code Questions
description: Gives CLI and MCP examples for asking code, dependency, and call-graph questions.
type: how-to
audience: practitioner
entrypoint: true
landing: false
-->

# Ask Code Questions

Start with a symbol, file, repository, or phrase. Eshu works best when the
question names the thing you want to inspect.

## CLI Examples

These commands call the HTTP API:

```bash
eshu analyze callers process_payment
eshu analyze calls process_payment
eshu analyze chain main process_payment
eshu analyze deps shared-auth-lib
eshu analyze dead-code --repo payments-api
eshu stats payments-api
```

If you are running locally, start Docker Compose or another API process first.
The local Compose API defaults to `http://localhost:8080`.

Use `--repo` or `--repo-id` on relationship commands when a symbol name is
common across repositories.

## MCP Examples

Ask your assistant questions like:

- "Find `process_payment` and show where it is defined."
- "Who calls this function across indexed repos?"
- "Show the shortest call chain from `main` to this handler."
- "Find dead-code candidates in this repository."
- "Which files import this package?"
- "What is the blast radius if this module changes?"

Ask for evidence when you need to make a decision:

> Use Eshu. Search the indexed repos, show the files and symbols involved, and
> explain what evidence supports the answer.

## Read Next

- [Starter Prompts](../guides/starter-prompts.md) for copy-ready questions.
- [MCP Guide](../guides/mcp-guide.md) for assistant tool-selection patterns.
- [CLI Reference](../reference/cli-reference.md) for exact flags and syntax.
