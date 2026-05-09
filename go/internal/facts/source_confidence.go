package facts

import "fmt"

const (
	// SourceConfidenceObserved marks facts read directly from a source artifact.
	SourceConfidenceObserved = "observed"
	// SourceConfidenceReported marks facts returned by an external system or API.
	SourceConfidenceReported = "reported"
	// SourceConfidenceInferred marks facts concluded by correlating other evidence.
	SourceConfidenceInferred = "inferred"
	// SourceConfidenceDerived marks facts materialized from existing Eshu facts.
	SourceConfidenceDerived = "derived"
	// SourceConfidenceUnknown marks legacy or system fallback facts.
	SourceConfidenceUnknown = "unknown"
)

// ValidateSourceConfidence validates the durable source_confidence vocabulary.
func ValidateSourceConfidence(value string) error {
	switch value {
	case SourceConfidenceObserved,
		SourceConfidenceReported,
		SourceConfidenceInferred,
		SourceConfidenceDerived,
		SourceConfidenceUnknown:
		return nil
	default:
		return fmt.Errorf("source_confidence %q is unsupported", value)
	}
}
