# AGENTS.md — AWS runtime projector namespace

Read `README.md`, `doc.go`, and `../AGENTS.md` before changing this tree.
Keep this directory documentation-only. The `drift` leaf selects AWS
runtime-state trigger facts; root projector owns lookup construction,
fan-out order, queue writes, retries, and telemetry, and reducer handlers
own canonical materialization.
