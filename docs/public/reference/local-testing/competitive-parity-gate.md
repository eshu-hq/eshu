# Competitive Parity Gate

The competitive parity gate is the repeatable #3265 check for shipped Eshu
surfaces that were inspired by local peer comparisons. It proves the current
first-run report, operator digest, investigation evidence packet, and capability
catalog surfaces are still reachable and documented without reading old issue
history.

Run it from a source checkout:

```bash
cd go
go run ./cmd/eshu competitive-parity validate --repo-root .. --json
```

The command emits a `competitive_parity_gate.v1` artifact. Omit `--json` for the
Markdown artifact, or pass `--out <path>` to write either format to disk. The
artifact separates presence parity from usefulness parity: a surface can be
reachable and still fail when the artifact is not actionable, clear, reproducible,
or useful enough for a reader.

## What It Checks

The gate is deterministic and offline. It reads the Cobra command tree, the
embedded generated surface inventory, public docs, and local artifact builders.
It does not start Eshu runtimes, call providers, open Postgres or graph
connections, claim reducer work, or infer graph truth.

The default checklist covers:

- graphify-style report readability for first-run evidence and operator digest
  artifacts;
- CodeGraphContext-style portable artifact usability for investigation evidence
  packets;
- GitNexus-style agent workflow discoverability for the capability catalog API,
  MCP, and console surfaces.

It also exercises the shipped local artifact paths: first-run evidence rendering,
operator digest artifact validation, investigation evidence packet rendering,
the evidence-packet dogfood fixture, and capability catalog/surface inventory
decoding.

## Usefulness Scoring

Each surface also carries deterministic quality dimensions in JSON and Markdown:

- `actionability` checks for ranked next commands, suggested questions, or
  bounded follow-up calls.
- `evidence_clarity` checks that missing evidence, stale state, truncation,
  unsupported state, or partial state are visible.
- `reproducibility` checks for schema versions, scopes, source references,
  route/tool names, commands, citation handles, or artifact IDs that let a user
  reproduce the bounded evidence.
- `reader_usefulness` checks that the artifact explains what matters, what is
  missing, and how to read the result.
- `peer_baseline_coverage` checks that the public contract still preserves the
  peer-inspired UX goal for that surface family.

Quality scoring is offline and provider-free. It uses only the repo-owned public
contract text already read by the gate, so it is safe for CI and does not inspect
private graph, Postgres, source, or runtime state.

## Failure Meaning

A failure means a shipped surface is no longer reachable from one of the
expected contracts:

- CLI command path missing;
- API route missing from the surface inventory;
- MCP tool missing from the surface inventory;
- console page missing from the surface inventory;
- public contract doc missing or no longer naming required truth/missing
  evidence terms;
- local artifact builder, renderer, scorer, or catalog decoder failure.
- quality dimension missing the deterministic signals that make the artifact
  actionable, explicit about gaps, reproducible, or useful to a reader.

Residual work is linked to existing issues instead of creating duplicate tickets.
The investigation evidence packet row now validates the packet API routes
(`/api/v0/investigations/{supply-chain/impact,deployable-unit,drift}/packet`),
the `export_*_packet` MCP tools, and the console pages that render the packet
layers, so #3238 is recorded as delivered related work rather than an open
residual gap. The default checklist has no open residuals; a residual reappears
only when a future surface ships behind the contract.
