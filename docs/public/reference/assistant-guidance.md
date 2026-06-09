# Assistant Guidance Install

`eshu assistant` writes project-scoped instructions that tell AI assistants to
prefer Eshu's bounded MCP/API tools for graph-backed questions and to respect
Eshu truth labels, freshness, and missing evidence. Use it once per project so
Claude Code, Codex, Cursor, and compatible clients reach for Eshu before broad
raw-file search.

This is separate from `eshu mcp setup`, which configures an MCP client to launch
the Eshu server. `eshu assistant` only manages instruction files in your repo.

## Commands

| Command | Effect |
| --- | --- |
| `eshu assistant install` | Write or refresh the Eshu guidance block for every supported assistant. |
| `eshu assistant status` | Report, per platform, whether the guidance block is installed and current. |
| `eshu assistant uninstall` | Remove the Eshu guidance block, preserving other file content. |

Flags:

- `--path <dir>` operates on the given project root instead of the current
  directory.
- `--platform <id>` restricts the action to one assistant: `claude`, `codex`, or
  `cursor`. An unknown id is rejected.

## Target Files

| Platform | File | Committed |
| --- | --- | --- |
| Claude Code | `CLAUDE.md` | yes |
| Codex / AGENTS.md | `AGENTS.md` | yes |
| Cursor | `.cursor/rules/eshu.mdc` | yes |

## Managed Block

Guidance lives inside a clearly delimited managed block between
`<!-- BEGIN ESHU GUIDANCE -->` and `<!-- END ESHU GUIDANCE -->`. Install and
reinstall rewrite only the bytes inside those markers, so any content you keep
before or after the block is preserved. Reinstall is idempotent: running it
again with the same guidance leaves the file byte-for-byte identical.

`uninstall` removes only the managed block and its markers. It deletes a file
only when that file held nothing but the Eshu block (so Eshu effectively created
it). A file with your own content keeps that content with just the block
stripped, and a file Eshu did not create is never deleted.

## Generated Guidance

The block instructs assistants to:

- Call the bounded Eshu tool first for graph-backed questions and fall back to
  raw-file search only when no Eshu tool fits.
- Keep calls bounded with the narrowest known `repo_id`, service, environment,
  resource, file, or symbol, and to honor `truncated`, `next_offset`, and
  `next_cursor`.
- Start from concrete first prompts such as building a service story or tracing a
  deployment chain.
- Respect Eshu truth labels: read `truth.level` (exact, derived, fallback) and
  `truth.freshness.state` (fresh, stale, building, unavailable), and state when
  evidence is missing rather than inventing graph edges.

See the [MCP Guide](../guides/mcp-guide.md),
[Starter Prompts](../guides/starter-prompts.md), and
[Truth Label Protocol](truth-label-protocol.md) for the canonical language the
guidance references.

## Committing Guidance

After an install that changed a committed file, the command prints `git add`
hints so teammates and CI agents share the same guidance:

```text
Commit the guidance so teammates and CI agents share it:
  git add CLAUDE.md
```

## Related Docs

- [CLI Reference](cli-reference.md)
- [MCP Guide](../guides/mcp-guide.md)
- [Starter Prompts](../guides/starter-prompts.md)
- [Truth Label Protocol](truth-label-protocol.md)
