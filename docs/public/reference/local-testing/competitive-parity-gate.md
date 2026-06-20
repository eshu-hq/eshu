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
Markdown artifact, or pass `--out <path>` to write either format to disk.

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

Residual work is linked to existing issues instead of creating duplicate tickets.
The investigation evidence packet row currently points to #3238 when packet
surfaces exist but are not yet exposed through every target surface.
