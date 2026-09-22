# internal/reducer/code/value/refresh

## Purpose

Re-runs the global value-flow fixpoint after late producers land (issues
#6785, #6923). `Handler.Handle` executes one `code_value_flow_refresh` intent
by calling only the fixpoint projector: summaries, sources, and graph ids are
unchanged by the producers whose completion enqueues the refresh, and the
fixpoint reloads them globally before solving.

Before that solve, `Handler` runs one input-liveness fence (`InputsLiveness`,
issue #6923): while any active-generation writer of the cloud-sink chain the
fixpoint reads is nonterminal, `Handle` refuses instead of reading partial
graph state — `Retryable()`, non-counting `value_flow_inputs_not_ready`
(`crossscope.ValueFlowInputsNotReadyFailureClass`) — until either the fence
clears or elapsed time since the singleton's own cycle anchor reaches
`crossscope.ProducerReadinessMaxWait`, at which point it solves anyway
(outcome `abandoned`). `code_function_summary` is now the fifth producer that
reopens this singleton: it stopped solving the fixpoint inline, so every
trigger of the global solve coalesces onto this one fenced item instead of
racing several inline solves — the row-set wobble issue #6923 reports, and
the concurrent-writer race issue #6880 filed separately.

## Ownership boundary

**Owns:** the refresh intent handler (`Handler`), its projector port
(`FixpointProjector`), and its input-liveness fence port (`InputsLiveness`).

**Does not own:** the fixpoint solver itself (`code/value`), the durable
completion fanout that reopens the singleton item (`storage/postgres`,
`reducer/crossscope`), the producer ACK emission that enqueues the events
(including `code_function_summary`'s), the fence's SQL implementation
(`storage/postgres.ValueFlowInputsLivenessStore`), the readiness-class
enrollment and starvation-bound helpers (`reducer/crossscope`), or the
reducer-root handler registration (`defaults*.go`, `cmd/reducer` wiring).
