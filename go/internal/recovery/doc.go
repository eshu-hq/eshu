// Package recovery owns replay and refinalize operations for the facts-first
// write plane.
//
// "Recovery" here means replaying durable projector or reducer work items
// through the queue, not direct graph mutation. ReplayFailed resets failed
// work items back to pending for the requested stage; DrainBacklog is the safe
// dead-letter backlog drain that defaults to the transient retry_exhausted
// bucket, refuses manual-review (poison) classes sourced from the projector
// triage, and reports backlog depth before replaying so an operator can watch
// progress; Refinalize re-enqueues projector work for an explicit list of scopes
// so their active generations are projected again. ReplayCollectorGenerations
// marks collector generation
// commit failures for source-level replay when the failure happened before
// durable projector work items existed; the source collector resolves those
// rows after a later successful commit for the same scope. Collector generation
// replay requires a non-blank collector kind before the store is called.
package recovery
