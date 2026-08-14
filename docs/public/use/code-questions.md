<!-- docs-catalog
title: Ask code questions
description: Gives CLI and MCP examples for asking code, dependency, and call-graph questions.
type: how-to
audience: practitioner
entrypoint: true
landing: false
-->

# Ask code questions

Start with a symbol, file, repository, or phrase. Eshu works best when the
question names the thing you want to inspect.

Before using the CLI examples, start an Eshu API and index the repository you
want to query. Local Compose serves the API at `http://localhost:8080` by
default. The `eshu` CLI must also be installed and available on `PATH`.

## Ask from the CLI

These commands call the HTTP API:

```bash
eshu analyze callers process_payment
eshu analyze calls process_payment
eshu analyze chain main process_payment
eshu analyze deps shared-auth-lib
eshu analyze dead-code --repo payments-api
eshu stats payments-api
```

Use `--repo` or `--repo-id` on relationship commands when a symbol name is
common across repositories.

## Ask from an MCP client

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

## Verify the answer

Check that the response names the repository, files, or symbols that support
it. If a name exists in more than one repository, repeat the command with
`--repo` or `--repo-id` and confirm that the bounded result matches your scope.

## Read next

- [Starter Prompts](../guides/starter-prompts.md) for copy-ready questions.
- [MCP Guide](../guides/mcp-guide.md) for assistant tool-selection patterns.
- [CLI Reference](../reference/cli-reference.md) for exact flags and syntax.
