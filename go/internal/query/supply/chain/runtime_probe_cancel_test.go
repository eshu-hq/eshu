// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"errors"
	"fmt"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

const probeCancelDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// cancelingCloudInventory fails the digest read with fail(), after calling
// cancel when it is set: a client walking away mid-read.
type cancelingCloudInventory struct {
	stubCloudInventory
	cancel func()
	err    error
}

func (s *cancelingCloudInventory) CurrentAuthorizedCloudResourcesByDigest(
	context.Context, []string, bool, []string, []string,
) ([]CloudResourceRuntimeDigestMatch, error) {
	if s.cancel != nil {
		s.cancel()
	}
	return nil, s.err
}

// cancelingKubernetesInventory fails the workload ledger read the same way.
type cancelingKubernetesInventory struct {
	cancel func()
	err    error
}

func (s *cancelingKubernetesInventory) CurrentAuthorizedKubernetesRuntimeWorkloads(
	context.Context, []KubernetesRuntimeCandidate, bool, []string, []string,
) ([]KubernetesRuntimeWorkloadMatch, error) {
	if s.cancel != nil {
		s.cancel()
	}
	return nil, s.err
}

// probeSpanExceptions runs probe under a recording tracer and returns how many
// exception events the probe's child span recorded.
func probeSpanExceptions(t *testing.T, spanName string, probe func(context.Context) error) int {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("supply-chain-runtime-probe-cancel-test")
	t.Cleanup(func() { queryHandlerTracer = previousTracer })

	if err := probe(context.Background()); err == nil {
		t.Fatal("probe error = nil, want the injected failure")
	}
	exceptions, found := 0, false
	for _, span := range recorder.Ended() {
		if span.Name() != spanName {
			continue
		}
		found = true
		for _, event := range span.Events() {
			if event.Name == "exception" {
				exceptions++
			}
		}
	}
	if !found {
		t.Fatalf("span %q not recorded", spanName)
	}
	return exceptions
}

// TestRuntimeProbeChildSpansSkipClientCancel pins that the cloud and
// Kubernetes runtime probe child spans record a backend fault and an inner
// cancel on a live request, but not a client cancel, matching the request
// span (#7674).
//
// Not parallel: it swaps the package-level queryHandlerTracer.
func TestRuntimeProbeChildSpansSkipClientCancel(t *testing.T) {
	rows := func() []impact.FindingRow {
		return []impact.FindingRow{{FindingID: "f-1", SubjectDigest: probeCancelDigest}}
	}
	access := querycontract.RepositoryAccessFilter{AllScopes: true}
	probes := []struct {
		name     string
		spanName string
		run      func(ctx context.Context, cancel func(), err error) error
	}{
		{
			name: "cloud ledger read", spanName: "supply_chain.cloud_runtime_probe",
			run: func(ctx context.Context, cancel func(), err error) error {
				h := &Handler{CloudResourceInventory: &cancelingCloudInventory{cancel: cancel, err: err}}
				_, probeErr := h.probeSupplyChainCloudRuntimeResources(ctx, access, []string{probeCancelDigest})
				return probeErr
			},
		},
		{
			name: "kubernetes graph read", spanName: "supply_chain.kubernetes_runtime_probe",
			run: func(ctx context.Context, cancel func(), err error) error {
				reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
					if cancel != nil {
						cancel()
					}
					return nil, err
				}}
				h := &Handler{Neo4j: reader, KubernetesWorkloadInventory: &cancelingKubernetesInventory{}}
				return h.applySupplyChainKubernetesRuntimeEvidence(ctx, access, rows())
			},
		},
		{
			name: "kubernetes ledger read", spanName: "supply_chain.kubernetes_runtime_probe",
			run: func(ctx context.Context, cancel func(), err error) error {
				reader := &graph.FakeKubernetesRuntimeGraph{Rows: []map[string]any{{
					"matched_digest": probeCancelDigest, "workload_uid": "kw-1",
					"edge_scope_id": "scope-edge", "edge_generation_id": "gen-edge",
				}}}
				h := &Handler{Neo4j: reader, KubernetesWorkloadInventory: &cancelingKubernetesInventory{cancel: cancel, err: err}}
				return h.applySupplyChainKubernetesRuntimeEvidence(ctx, access, rows())
			},
		},
	}
	for _, probe := range probes {
		t.Run(probe.name+"/backend failure", func(t *testing.T) {
			got := probeSpanExceptions(t, probe.spanName, func(ctx context.Context) error {
				return probe.run(ctx, nil, errors.New("backend: connection reset"))
			})
			if got != 1 {
				t.Fatalf("exception events = %d, want 1", got)
			}
		})
		t.Run(probe.name+"/inner cancel on a live request", func(t *testing.T) {
			// A context.Canceled from an inner context while the caller's
			// request is still live is a server fault, not a client cancel.
			got := probeSpanExceptions(t, probe.spanName, func(ctx context.Context) error {
				return probe.run(ctx, nil, fmt.Errorf("backend: %w", context.Canceled))
			})
			if got != 1 {
				t.Fatalf("exception events = %d, want 1 for an inner cancel", got)
			}
		})
		t.Run(probe.name+"/client cancel", func(t *testing.T) {
			got := probeSpanExceptions(t, probe.spanName, func(ctx context.Context) error {
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				return probe.run(ctx, cancel, fmt.Errorf("backend: %w", context.Canceled))
			})
			if got != 0 {
				t.Fatalf("exception events = %d, want 0 for a client cancel", got)
			}
		})
	}
}
