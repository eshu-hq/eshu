// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestBuildReducerServiceWiresCloudRetractSeams guards against the #6892 P0:
// the production reducer.DefaultHandlers{...} literal in buildReducerService
// omitted PriorGeneration, CloudResourceNodeRetracter, and
// EC2InstanceNodeRetracter, so both cloud handlers' nil guards tripped on
// every generation and the #6887 generation-diff retract never executed in
// production while unit tests (which wire the seams manually) stayed green.
//
// The test drives the AWS and EC2 materialization domains through the actual
// production buildReducerService wiring (not a hand-populated
// DefaultHandlers literal). The fake serves one prior generation holding one
// predecessor-only fact per slice and refuses the retract transaction begin
// with a probe error, so execution reaches the production graphowner
// retracter if and only if all three seams are wired: the returned error must
// be the retracter's begin failure wrapped by the generation-diff helper. An
// omitted seam skips the retract silently and the intent fails later with a
// different error (or succeeds), failing this assertion.
//
// Coverage boundary: the probe trips at Begin, so this test proves the three
// DefaultHandlers seams reach the gate — it does not prove the gate's inner
// store wiring, which retract_test.go covers with fakes.
func TestBuildReducerServiceWiresCloudRetractSeams(t *testing.T) {
	t.Parallel()

	database := &retractWiringDB{}
	service, err := buildReducerService(context.Background(), database, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(database), stubCypherReader{}, stubCypherReader{}, func(string) string { return "" }, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	// Arm the begin probe only after startup: construction must succeed with
	// a beginner-capable database exactly as it does in production, and the
	// probe must trip only when the retract critical section opens.
	database.refuseBegin = true

	for _, tc := range []struct {
		domain  reducer.Domain
		wantErr string
	}{
		{domain: reducer.DomainAWSResourceMaterialization, wantErr: "retract dead cloud resource nodes"},
		{domain: reducer.DomainEC2InstanceNodeMaterialization, wantErr: "retract dead ec2 instance nodes"},
	} {
		t.Run(string(tc.domain), func(t *testing.T) {
			intent := reducer.Intent{
				IntentID:        "intent-retract-wiring-" + string(tc.domain),
				ScopeID:         "scope-123",
				GenerationID:    "generation-456",
				SourceSystem:    "git",
				Domain:          tc.domain,
				Cause:           "wiring probe",
				EntityKeys:      []string{"scope-123"},
				RelatedScopeIDs: []string{"scope-123"},
				EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
				AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
			}
			_, execErr := service.Executor.Execute(context.Background(), intent)
			if execErr == nil || !strings.Contains(execErr.Error(), tc.wantErr) {
				t.Fatalf("Executor.Execute() error = %v, want error containing %q (retract seam not reached through production wiring)", execErr, tc.wantErr)
			}
		})
	}
}

// retractWiringDB serves the #6887 wiring probe: the generation-456 current
// generation stays empty (delegated to the embedded fake), while the
// generation-123 prior generation carries one predecessor-only fact per cloud
// slice. Begin refuses once armed so the production retracter's transaction
// open fails with a recognizable probe error instead of touching a real
// backend.
type retractWiringDB struct {
	fakeReducerDB
	refuseBegin bool
}

func (f *retractWiringDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if strings.Contains(query, "SELECT generation_id") && strings.Contains(query, "FROM scope_generations") {
		return &retractWiringStringRows{value: "generation-123"}, nil
	}
	if strings.Contains(query, "FROM fact_records") && len(args) > 2 {
		if gen, ok := args[1].(string); ok && gen == "generation-123" {
			if kinds, ok := args[2].([]string); ok && len(kinds) > 0 {
				switch kinds[0] {
				case facts.AWSResourceFactKind:
					return retractWiringEnvelopeRows(facts.AWSResourceFactKind, map[string]any{
						"account_id":    "111122223333",
						"region":        "us-east-1",
						"resource_type": "aws_ec2_vpc",
						"resource_id":   "vpc-deleted",
					})
				case facts.EC2InstancePostureFactKind:
					return retractWiringEnvelopeRows(facts.EC2InstancePostureFactKind, map[string]any{
						"account_id":                  "111122223333",
						"region":                      "us-east-1",
						"service_kind":                "ec2",
						"resource_type":               "aws_ec2_instance",
						"arn":                         "arn:aws:ec2:us-east-1:111122223333:instance/i-deleted",
						"instance_id":                 "i-deleted",
						"state":                       "running",
						"imds_v2_required":            true,
						"imds_http_endpoint":          "enabled",
						"imds_http_put_hop_limit":     float64(1),
						"user_data_present":           false,
						"detailed_monitoring_enabled": false,
						"ebs_optimized":               true,
						"public_ip_associated":        false,
						"instance_profile_arn":        "arn:aws:iam::111122223333:instance-profile/app",
						"tenancy":                     "default",
						"nitro_enclave_enabled":       false,
						"correlation_anchors":         []any{"i-deleted"},
					})
				}
			}
		}
	}
	return f.fakeReducerDB.QueryContext(ctx, query, args...)
}

func (f *retractWiringDB) Begin(context.Context) (db.Transaction, error) {
	if f.refuseBegin {
		return nil, errors.New("cloud retract wiring probe: refuse retract transaction begin")
	}
	return nil, nil
}

// retractWiringStringRows returns a single one-column string row, modeling
// the prior-generation lookup.
type retractWiringStringRows struct {
	value    string
	consumed bool
}

func (r *retractWiringStringRows) Next() bool {
	if r.consumed {
		return false
	}
	r.consumed = true
	return true
}

func (r *retractWiringStringRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("scan: got wrong dest count, want 1")
	}
	s, ok := dest[0].(*string)
	if !ok {
		return errors.New("scan: dest is not *string")
	}
	*s = r.value
	return nil
}

func (r *retractWiringStringRows) Err() error   { return nil }
func (r *retractWiringStringRows) Close() error { return nil }

// retractWiringEnvelopeRows returns one fact_records row in the exact column
// order scanFactEnvelope scans: twelve strings, one int64, one timestamp,
// one bool, and the JSON payload bytes.
func retractWiringEnvelopeRows(factKind string, payload map[string]any) (db.Rows, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &retractWiringSingleEnvelopeRows{
		columns: []any{
			"fact-retract-wiring", "scope-123", "generation-123", factKind,
			"stable-retract-wiring", "1.0.0", "wiring-probe",
			int64(0),
			"source-confidence-probe", "wiring-probe", "fact-key-probe",
			"source-uri-probe", "record-id-probe",
			time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC), false, raw,
		},
	}, nil
}

type retractWiringSingleEnvelopeRows struct {
	columns  []any
	consumed bool
}

func (r *retractWiringSingleEnvelopeRows) Next() bool {
	if r.consumed {
		return false
	}
	r.consumed = true
	return true
}

func (r *retractWiringSingleEnvelopeRows) Scan(dest ...any) error {
	if len(dest) != len(r.columns) {
		return errors.New("scan: got wrong dest count for fact envelope")
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			s, ok := r.columns[i].(string)
			if !ok {
				return errors.New("scan: column is not a string")
			}
			*d = s
		case *int64:
			n, ok := r.columns[i].(int64)
			if !ok {
				return errors.New("scan: column is not an int64")
			}
			*d = n
		case *time.Time:
			ts, ok := r.columns[i].(time.Time)
			if !ok {
				return errors.New("scan: column is not a timestamp")
			}
			*d = ts
		case *bool:
			b, ok := r.columns[i].(bool)
			if !ok {
				return errors.New("scan: column is not a bool")
			}
			*d = b
		case *[]byte:
			raw, ok := r.columns[i].([]byte)
			if !ok {
				return errors.New("scan: column is not bytes")
			}
			*d = raw
		default:
			return errors.New("scan: unsupported dest type")
		}
	}
	return nil
}

func (r *retractWiringSingleEnvelopeRows) Err() error   { return nil }
func (r *retractWiringSingleEnvelopeRows) Close() error { return nil }
