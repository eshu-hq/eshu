// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// grantedEnv installs a granted core-kind component, enables it, optionally
// revokes the grant, and returns the runtime env for the worker.
func grantedEnv(t *testing.T, revoke bool) func(string) string {
	t.Helper()

	const (
		componentID = "dev.eshu.examples.granted"
		version     = "0.1.0"
		coreKind    = "aws_resource"
	)
	grant := component.ProducerGrant{
		ProducerID:     componentID,
		Version:        version,
		Kind:           coreKind,
		SchemaVersions: []string{"1.0.0"},
		Scope:          "scorecard",
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}
	home := t.TempDir()
	registry := component.NewRegistry(home)
	if err := registry.RecordGrant(grant); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	policy := component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{componentID},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "dev",
	}
	verification := policy.VerifyWithGrants(
		context.Background(), grantedComponentManifest(componentID, version, coreKind), []component.ProducerGrant{grant},
	)
	if _, err := registry.Install(writeGrantedManifest(t, componentID, version, coreKind), verification); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if _, err := registry.Enable(componentID, component.Activation{
		InstanceID: "granted-local", Mode: "scheduled", ClaimsEnabled: true,
		ConfigPath: writeComponentExtensionConfig(t),
	}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}
	if revoke {
		if err := registry.RevokeGrant(componentID, version, coreKind, "scorecard"); err != nil {
			t.Fatalf("RevokeGrant() error = %v, want nil", err)
		}
	}
	return mapEnv(map[string]string{
		envComponentHome:            home,
		envComponentTrustMode:       component.TrustModeAllowlist,
		envComponentAllowIDs:        componentID,
		envComponentAllowPublishers: "eshu-hq",
		envComponentCoreVersion:     "dev",
		envCollectorInstanceID:      "granted-local",
	})
}

func grantPoints(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	points := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != contract.MetricProducerGrantDecisions {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				get := func(key string) string {
					v, _ := dp.Attributes.Value(attributeKey(key))
					return v.AsString()
				}
				points[get("decision")+"/"+get("stage")+"/"+get("reason")] += dp.Value
			}
		}
	}
	return points
}

func grantInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	return inst, reader
}

// TestBuildClaimedServiceRecordsGrantDecisions proves the worker wires the
// grant observer end to end: a granted activation records the readback and
// activation allows (and logs the activation allow), so an operator can see
// approvals, not just failures.
func TestBuildClaimedServiceRecordsGrantDecisions(t *testing.T) {
	t.Parallel()

	inst, reader := grantInstruments(t)
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	if _, err := buildClaimedService(
		&fakeExecQueryer{}, grantedEnv(t, false), noop.NewTracerProvider().Tracer("test"), inst, logger,
	); err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	got := grantPoints(t, reader)
	for _, want := range []string{"allow/readback/granted", "allow/activation/granted"} {
		if got[want] != 1 {
			t.Fatalf("decision points = %v, want %s == 1", got, want)
		}
	}
	if !bytes.Contains(logs.Bytes(), []byte(`"producer_grant.decision":"allow"`)) {
		t.Fatalf("logs = %s, want an activation allow record", logs.String())
	}
}

// TestBuildClaimedServiceRevokedGrantFailsClosedAndRecordsDeny is the wiring
// level negative-authorization proof: a revoked grant must still stop the
// worker (fail closed, no service built) AND record decision=deny at the
// readback stage with reason revoked.
func TestBuildClaimedServiceRevokedGrantFailsClosedAndRecordsDeny(t *testing.T) {
	t.Parallel()

	inst, reader := grantInstruments(t)
	service, err := buildClaimedService(
		&fakeExecQueryer{}, grantedEnv(t, true), noop.NewTracerProvider().Tracer("test"), inst, nil,
	)
	if err == nil {
		t.Fatalf("buildClaimedService() error = nil (service %v), want fail-closed for a revoked grant", service)
	}
	if got := grantPoints(t, reader); got["deny/readback/revoked"] != 1 {
		t.Fatalf("decision points = %v, want deny/readback/revoked == 1", got)
	}
}

func attributeKey(key string) attribute.Key { return attribute.Key(key) }
