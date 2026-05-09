// Package facts defines the frozen Go data-plane fact envelope contracts.
package facts

import (
	"fmt"
	"time"
)

// Ref identifies the source-local record that produced one fact.
type Ref struct {
	SourceSystem   string
	ScopeID        string
	GenerationID   string
	FactKey        string
	SourceURI      string
	SourceRecordID string
}

// ScopeGenerationKey returns the durable scope-generation boundary for this ref.
func (r Ref) ScopeGenerationKey() string {
	return scopeGenerationKey(r.ScopeID, r.GenerationID)
}

// Envelope is the durable Go representation of a fact envelope.
type Envelope struct {
	FactID           string
	ScopeID          string
	GenerationID     string
	FactKind         string
	StableFactKey    string
	SchemaVersion    string
	CollectorKind    string
	FencingToken     int64
	SourceConfidence string
	ObservedAt       time.Time
	Payload          map[string]any
	IsTombstone      bool
	SourceRef        Ref
}

// ScopeGenerationKey returns the durable scope-generation boundary for this envelope.
func (e Envelope) ScopeGenerationKey() string {
	return scopeGenerationKey(e.ScopeID, e.GenerationID)
}

// Clone returns a replay-safe copy of the envelope.
func (e Envelope) Clone() Envelope {
	cloned := e
	if e.Payload != nil {
		cloned.Payload = cloneMap(e.Payload)
	}

	return cloned
}

func scopeGenerationKey(scopeID, generationID string) string {
	return fmt.Sprintf("%s:%s", scopeID, generationID)
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}

	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = cloneValue(value)
	}

	return cloned
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i := range typed {
			cloned[i] = cloneValue(typed[i])
		}
		return cloned
	default:
		return typed
	}
}
