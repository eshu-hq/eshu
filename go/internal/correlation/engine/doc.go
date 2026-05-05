// Package engine evaluates a correlation rule pack against a candidate slice
// and produces deterministic admission outcomes.
//
// `Evaluate` validates the pack, sorts rules by `(Priority, Name)`, runs
// `admission.Evaluate` per candidate, attaches rejection reasons for
// confidence and structural failures, breaks ties between admitted
// candidates that share a `CorrelationKey` by confidence and ID, and returns
// results sorted by `(CorrelationKey, State, ID)`. Output ordering is part of
// the contract; callers and tests rely on it for stable explain rendering and
// replay.
package engine
