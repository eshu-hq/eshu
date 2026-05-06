// Package tags defines the reducer-owned tag normalization seam and the
// readiness publication helpers it shares with the phase publisher.
//
// `Normalizer` is the seam future tag substrates implement; the package
// converts a `NormalizationResult` into canonical-cloud
// `GraphProjectionPhaseState` rows and forwards them through the shared
// `reducer.GraphProjectionPhasePublisher`. RuntimeContractTemplate exposes
// the accepted scaffold as defensive copies; the package owns no
// normalization logic itself.
package tags
