// Package recovery owns replay and refinalize operations for the facts-first
// write plane.
//
// "Recovery" here means replaying durable projector or reducer work items
// through the queue, not direct graph mutation. ReplayFailed resets failed
// work items back to pending for the requested stage; Refinalize re-enqueues
// projector work for an explicit list of scopes so their active generations
// are projected again.
package recovery
