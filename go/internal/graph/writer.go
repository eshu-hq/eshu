package graph

import (
	"context"
	"fmt"
)

// Record captures one source-local graph write candidate.
type Record struct {
	RecordID   string
	Kind       string
	Attributes map[string]string
	Deleted    bool
}

// Clone returns a copy-safe record value.
func (r Record) Clone() Record {
	cloned := r
	if r.Attributes != nil {
		cloned.Attributes = cloneStringMap(r.Attributes)
	}

	return cloned
}

// Materialization is the source-local graph payload for one scope generation.
type Materialization struct {
	ScopeID      string
	GenerationID string
	SourceSystem string
	Records      []Record
}

// ScopeGenerationKey returns the durable scope-generation boundary.
func (m Materialization) ScopeGenerationKey() string {
	return fmt.Sprintf("%s:%s", m.ScopeID, m.GenerationID)
}

// Clone returns a copy-safe materialization.
func (m Materialization) Clone() Materialization {
	cloned := m
	if len(m.Records) > 0 {
		cloned.Records = make([]Record, len(m.Records))
		for i := range m.Records {
			cloned.Records[i] = m.Records[i].Clone()
		}
	}

	return cloned
}

// Result summarizes one source-local graph write.
type Result struct {
	ScopeID      string
	GenerationID string
	RecordCount  int
	DeletedCount int
}

// Writer is the narrow source-local graph write contract.
type Writer interface {
	Write(context.Context, Materialization) (Result, error)
}

// MemoryWriter is a tiny in-memory writer useful in tests and adapters.
type MemoryWriter struct {
	Writes []Materialization
}

// Write stores a clone of the materialization and returns a derived result.
func (w *MemoryWriter) Write(_ context.Context, materialization Materialization) (Result, error) {
	if w == nil {
		return Result{}, fmt.Errorf("memory writer is nil")
	}

	cloned := materialization.Clone()
	w.Writes = append(w.Writes, cloned)

	result := Result{
		ScopeID:      cloned.ScopeID,
		GenerationID: cloned.GenerationID,
		RecordCount:  len(cloned.Records),
	}
	for _, record := range cloned.Records {
		if record.Deleted {
			result.DeletedCount++
		}
	}

	return result, nil
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}

	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}

	return cloned
}
