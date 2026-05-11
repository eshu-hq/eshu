package tfconfigstate

import "fmt"

// DriftKind is the closed enum of Terraform config-vs-state drift outcomes the
// classifier may emit. The five values map directly to the `drift_kind` label
// on eshu_dp_correlation_drift_detected_total; the cardinality is intentionally
// bounded for telemetry safety. The empty string is the explicit "no drift"
// sentinel returned by Classify when config and state agree.
type DriftKind string

const (
	// DriftKindAddedInState fires when a resource address exists on the state
	// side but the config side carries no matching declaration in the joined
	// config snapshot.
	DriftKindAddedInState DriftKind = "added_in_state"
	// DriftKindAddedInConfig fires when a resource address exists on the
	// config side but the joined state snapshot has no matching instance.
	DriftKindAddedInConfig DriftKind = "added_in_config"
	// DriftKindAttributeDrift fires when both sides carry the address and at
	// least one allowlisted attribute differs between config and state.
	// Computed/unknown config values never raise this drift kind.
	DriftKindAttributeDrift DriftKind = "attribute_drift"
	// DriftKindRemovedFromState fires when the prior state generation carried
	// the address but the current state generation does not, while config
	// still declares it.
	DriftKindRemovedFromState DriftKind = "removed_from_state"
	// DriftKindRemovedFromConfig fires when the current state carries the
	// address but the latest joined config snapshot no longer declares it.
	DriftKindRemovedFromConfig DriftKind = "removed_from_config"
)

// AllDriftKinds returns the closed enum in deterministic order. Useful for
// telemetry cardinality tests that assert no values leak outside this set.
func AllDriftKinds() []DriftKind {
	return []DriftKind{
		DriftKindAddedInState,
		DriftKindAddedInConfig,
		DriftKindAttributeDrift,
		DriftKindRemovedFromState,
		DriftKindRemovedFromConfig,
	}
}

// Validate reports whether the value is one of the five recognized drift kinds.
// The empty string is not valid; callers that need to express "no drift" must
// not pass it through Validate.
func (k DriftKind) Validate() error {
	switch k {
	case DriftKindAddedInState,
		DriftKindAddedInConfig,
		DriftKindAttributeDrift,
		DriftKindRemovedFromState,
		DriftKindRemovedFromConfig:
		return nil
	default:
		return fmt.Errorf("unknown drift kind %q", k)
	}
}

// String returns the lowercase metric-label representation.
func (k DriftKind) String() string {
	return string(k)
}
