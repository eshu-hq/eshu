// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type admissionDecisionTestDB struct {
	decisions map[string]AdmissionDecision
	evidence  map[string]AdmissionDecisionEvidence
}

func newAdmissionDecisionTestDB() *admissionDecisionTestDB {
	return &admissionDecisionTestDB{
		decisions: make(map[string]AdmissionDecision),
		evidence:  make(map[string]AdmissionDecisionEvidence),
	}
}

func (db *admissionDecisionTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO admission_decisions"):
		decision := AdmissionDecision{
			DecisionID:       args[0].(string),
			Domain:           args[1].(string),
			State:            AdmissionDecisionState(args[2].(string)),
			DomainState:      args[3].(string),
			ScopeID:          args[4].(string),
			GenerationID:     args[5].(string),
			AnchorKind:       args[6].(string),
			AnchorID:         args[7].(string),
			CandidateKind:    args[8].(string),
			CandidateID:      args[9].(string),
			ConfidenceScore:  args[10].(float64),
			ConfidenceBucket: args[11].(string),
			ConfidenceBasis:  args[12].(string),
			FreshnessState:   args[13].(string),
			FreshnessCause:   args[15].(string),
			RedactionState:   args[17].(string),
			RedactionReason:  args[18].(string),
			PayloadVersion:   args[21].(string),
			DecidedAt:        args[22].(time.Time),
			UpdatedAt:        args[23].(time.Time),
		}
		if observedAt, ok := args[14].(time.Time); ok {
			decision.FreshnessObservedAt = &observedAt
		}
		mustUnmarshalAdmissionDecisionTestJSON(args[16], &decision.SourceHandles)
		mustUnmarshalAdmissionDecisionTestJSON(args[19], &decision.CanonicalWrite)
		mustUnmarshalAdmissionDecisionTestJSON(args[20], &decision.RecommendedAction)
		db.decisions[decision.DecisionID] = decision
		return result{}, nil

	case strings.Contains(query, "INSERT INTO admission_decision_evidence"):
		row := AdmissionDecisionEvidence{
			EvidenceID:   args[0].(string),
			DecisionID:   args[1].(string),
			SourceHandle: args[2].(string),
			EvidenceKind: args[3].(string),
			CreatedAt:    args[5].(time.Time),
		}
		mustUnmarshalAdmissionDecisionTestJSON(args[4], &row.Detail)
		db.evidence[row.EvidenceID] = row
		return result{}, nil

	case strings.Contains(query, "CREATE TABLE"):
		return result{}, nil

	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (db *admissionDecisionTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	switch {
	case strings.Contains(query, "FROM admission_decisions"):
		domain := args[0].(string)
		scopeID := args[1].(string)
		generationID := args[2].(string)
		state := args[3].(string)
		anchorKind := args[4].(string)
		anchorID := args[5].(string)
		limit := args[6].(int)
		rows := make([]AdmissionDecision, 0)
		for _, decision := range db.decisions {
			if decision.Domain != domain || decision.ScopeID != scopeID || decision.GenerationID != generationID {
				continue
			}
			if state != "" && string(decision.State) != state {
				continue
			}
			if anchorKind != "" && decision.AnchorKind != anchorKind {
				continue
			}
			if anchorID != "" && decision.AnchorID != anchorID {
				continue
			}
			rows = append(rows, decision)
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].UpdatedAt.Equal(rows[j].UpdatedAt) {
				return rows[i].DecisionID < rows[j].DecisionID
			}
			return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
		})
		if len(rows) > limit {
			rows = rows[:limit]
		}
		return admissionDecisionRowsFromDecisions(rows), nil

	case strings.Contains(query, "FROM admission_decision_evidence"):
		decisionID := args[0].(string)
		limit := args[1].(int)
		rows := make([]AdmissionDecisionEvidence, 0)
		for _, evidence := range db.evidence {
			if evidence.DecisionID == decisionID {
				rows = append(rows, evidence)
			}
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
				return rows[i].EvidenceID < rows[j].EvidenceID
			}
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		})
		if len(rows) > limit {
			rows = rows[:limit]
		}
		return admissionDecisionRowsFromEvidence(rows), nil

	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func mustUnmarshalAdmissionDecisionTestJSON(arg any, dest any) {
	bytes, ok := arg.([]byte)
	if !ok || len(bytes) == 0 {
		return
	}
	if err := json.Unmarshal(bytes, dest); err != nil {
		panic(err)
	}
}

func admissionDecisionRowsFromDecisions(rows []AdmissionDecision) Rows {
	data := make([][]any, 0, len(rows))
	for _, decision := range rows {
		sourceHandles, _ := json.Marshal(decision.SourceHandles)
		canonicalWrite, _ := json.Marshal(decision.CanonicalWrite)
		nextAction, _ := json.Marshal(decision.RecommendedAction)
		var observedAt any
		if decision.FreshnessObservedAt != nil {
			observedAt = *decision.FreshnessObservedAt
		}
		data = append(data, []any{
			decision.DecisionID,
			decision.Domain,
			string(decision.State),
			decision.DomainState,
			decision.ScopeID,
			decision.GenerationID,
			decision.AnchorKind,
			decision.AnchorID,
			decision.CandidateKind,
			decision.CandidateID,
			decision.ConfidenceScore,
			decision.ConfidenceBucket,
			decision.ConfidenceBasis,
			decision.FreshnessState,
			observedAt,
			decision.FreshnessCause,
			sourceHandles,
			decision.RedactionState,
			decision.RedactionReason,
			canonicalWrite,
			nextAction,
			decision.PayloadVersion,
			decision.DecidedAt,
			decision.UpdatedAt,
		})
	}
	return &admissionDecisionRows{data: data, idx: -1}
}

func admissionDecisionRowsFromEvidence(rows []AdmissionDecisionEvidence) Rows {
	data := make([][]any, 0, len(rows))
	for _, row := range rows {
		detail, _ := json.Marshal(row.Detail)
		data = append(data, []any{
			row.EvidenceID,
			row.DecisionID,
			row.SourceHandle,
			row.EvidenceKind,
			detail,
			row.CreatedAt,
		})
	}
	return &admissionDecisionRows{data: data, idx: -1}
}

type admissionDecisionRows struct {
	data [][]any
	idx  int
}

func (r *admissionDecisionRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *admissionDecisionRows) Scan(dest ...any) error {
	if r.idx < 0 || r.idx >= len(r.data) {
		return fmt.Errorf("scan out of range")
	}
	row := r.data[r.idx]
	if len(dest) != len(row) {
		return fmt.Errorf("scan: got %d dest, have %d cols", len(dest), len(row))
	}
	for i, val := range row {
		switch d := dest[i].(type) {
		case *string:
			if val == nil {
				*d = ""
				continue
			}
			*d = val.(string)
		case *float64:
			*d = val.(float64)
		case *time.Time:
			*d = val.(time.Time)
		case *sql.NullTime:
			if val == nil {
				*d = sql.NullTime{}
				continue
			}
			*d = sql.NullTime{Time: val.(time.Time), Valid: true}
		case **time.Time:
			if val == nil {
				*d = nil
				continue
			}
			tm := val.(time.Time)
			*d = &tm
		case *[]byte:
			*d = val.([]byte)
		default:
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
	}
	return nil
}

func (r *admissionDecisionRows) Err() error {
	return nil
}

func (r *admissionDecisionRows) Close() error {
	return nil
}
