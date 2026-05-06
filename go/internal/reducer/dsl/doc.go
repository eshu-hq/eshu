// Package dsl defines the cross-source DSL evaluator seam for the reducer
// and the readiness-publication helpers it shares with the phase publisher.
//
// `Evaluator` and `DriftEvaluator` are the seams future DSL substrates
// implement; they receive a bounded `CanonicalView` and return an
// `EvaluationResult`. `EvaluationResult.PhaseStates` and
// `PublishEvaluationResult` convert the result into durable
// `GraphProjectionPhaseState` rows and forward them through the shared
// `reducer.GraphProjectionPhasePublisher`. RuntimeContractTemplate exposes the
// accepted scaffold as defensive copies; the package owns no evaluation logic
// itself and only defines the contract that downstream evaluators must honor.
package dsl
