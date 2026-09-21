# internal/reducer/code/value/refresh

## Purpose

Re-runs the global value-flow fixpoint after late producers land (issue
#6785). `Handler.Handle` executes one `code_value_flow_refresh` intent by
calling only the fixpoint projector: summaries, sources, and graph ids are
unchanged by the producers whose completion enqueues the refresh, and the
fixpoint reloads them globally before solving.

## Ownership boundary

**Owns:** the refresh intent handler (`Handler`) and its projector port
(`FixpointProjector`).

**Does not own:** the fixpoint solver itself (`code/value`), the durable
completion fanout that reopens the singleton item (`storage/postgres`,
`reducer/crossscope`), the producer ACK emission that enqueues the events, or
the reducer-root handler registration (`defaults*.go`, `cmd/reducer` wiring).
