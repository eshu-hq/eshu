// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package log_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

func TestTelemetryBackedKeysMatchContract(t *testing.T) {
	tests := []struct {
		name string
		attr slog.Attr
		key  string
		val  string
	}{
		{"ScopeID", log.ScopeID("s1"), telemetry.LogKeyScopeID, "s1"},
		{"ScopeKind", log.ScopeKind("repo"), telemetry.LogKeyScopeKind, "repo"},
		{"CollectorKind", log.CollectorKind("git"), telemetry.LogKeyCollectorKind, "git"},
		{"Domain", log.Domain("infra"), telemetry.LogKeyDomain, "infra"},
		{"GenerationID", log.GenerationID("gen-1"), telemetry.LogKeyGenerationID, "gen-1"},
		{"FailureClass", log.FailureClass("timeout"), telemetry.LogKeyFailureClass, "timeout"},
		{"PipelinePhase", log.PipelinePhase("parse"), telemetry.LogKeyPipelinePhase, "parse"},
		{"RequestID", log.RequestID("req-1"), telemetry.LogKeyRequestID, "req-1"},
		{"SourceSystem", log.SourceSystem("eshu"), telemetry.LogKeySourceSystem, "eshu"},
		{"PartitionKey", log.PartitionKey("pk"), telemetry.LogKeyPartitionKey, "pk"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.attr.Key; got != tt.key {
				t.Errorf("key = %q, want %q", got, tt.key)
			}
			if got := tt.attr.Value.String(); got != tt.val {
				t.Errorf("value = %q, want %q", got, tt.val)
			}
		})
	}
}

func TestPkgLogOwnedKeys(t *testing.T) {
	tests := []struct {
		name string
		attr slog.Attr
		key  string
		val  string
	}{
		{"Err", log.Err(errors.New("boom")), log.KeyError, "boom"},
		{"ErrStr", log.ErrStr("fail"), log.KeyError, "fail"},
		{"TenantID", log.TenantID("t1"), log.KeyTenantID, "t1"},
		{"RepoPath", log.RepoPath("/a/b"), log.KeyRepoPath, "/a/b"},
		{"Queue", log.Queue("main"), log.KeyQueue, "main"},
		{"IntentID", log.IntentID("i1"), log.KeyIntentID, "i1"},
		{"WorkerID", log.WorkerID("w1"), log.KeyWorkerID, "w1"},
		{"Component", log.Component("api"), log.KeyComponent, "api"},
		{"RuntimeRole", log.RuntimeRole("reducer"), log.KeyRuntimeRole, "reducer"},
		{"RepositoryID", log.RepositoryID("r1"), log.KeyRepositoryID, "r1"},
		{"WorkloadID", log.WorkloadID("wl1"), log.KeyWorkloadID, "wl1"},
		{"ClusterID", log.ClusterID("c1"), log.KeyClusterID, "c1"},
		{"ElapsedSeconds", log.ElapsedSeconds(1.5), log.KeyElapsedSeconds, "1.5"},
		{"BatchSize", log.BatchSize(100), log.KeyBatchSize, "100"},
		{"SkipReason", log.SkipReason("no_match"), log.KeySkipReason, "no_match"},
		{"Language", log.Language("go"), log.KeyLanguage, "go"},
		{"Provider", log.Provider("aws"), log.KeyProvider, "aws"},
		{"Operation", log.Operation("upsert"), log.KeyOperation, "upsert"},
		{"Status", log.Status("ok"), log.KeyStatus, "ok"},
		{"EventKind", log.EventKind("deploy"), log.KeyEventKind, "deploy"},
		{"EventName", log.EventName("pushed"), log.KeyEventName, "pushed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.attr.Key; got != tt.key {
				t.Errorf("key = %q, want %q", got, tt.key)
			}
			if got := tt.attr.Value.String(); got != tt.val {
				t.Errorf("value = %q, want %q", got, tt.val)
			}
		})
	}
}

func TestWithHelpersMatchAttrConstructors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		withFn slog.Attr
		attrFn slog.Attr
	}{
		{log.WithTenant(ctx, "t1"), log.TenantID("t1")},
		{log.WithCollectorKind(ctx, "git"), log.CollectorKind("git")},
		{log.WithScopeID(ctx, "s1"), log.ScopeID("s1")},
		{log.WithDomain(ctx, "infra"), log.Domain("infra")},
		{log.WithGenerationID(ctx, "g1"), log.GenerationID("g1")},
		{log.WithFailureClass(ctx, "timeout"), log.FailureClass("timeout")},
		{log.WithPipelinePhase(ctx, "parse"), log.PipelinePhase("parse")},
	}

	for _, tt := range tests {
		t.Run(tt.withFn.Key, func(t *testing.T) {
			if tt.withFn.Key != tt.attrFn.Key {
				t.Errorf("With* key %q != Attr key %q", tt.withFn.Key, tt.attrFn.Key)
			}
			if tt.withFn.Value.String() != tt.attrFn.Value.String() {
				t.Errorf("With* value %q != Attr value %q", tt.withFn.Value.String(), tt.attrFn.Value.String())
			}
		})
	}
}

func TestEdgeCases(t *testing.T) {
	t.Run("Err_nil", func(t *testing.T) {
		attr := log.Err(nil)
		if attr.Key != "" {
			t.Errorf("nil error should produce empty attr, got key=%q", attr.Key)
		}
	})

	t.Run("ErrStr_empty", func(t *testing.T) {
		attr := log.ErrStr("")
		if attr.Key != log.KeyError {
			t.Errorf("key = %q, want %q", attr.Key, log.KeyError)
		}
	})

	t.Run("empty_strings", func(t *testing.T) {
		attrs := []slog.Attr{
			log.ScopeID(""),
			log.CollectorKind(""),
			log.TenantID(""),
		}
		for _, a := range attrs {
			if a.Value.String() != "" {
				t.Errorf("%s: expected empty value, got %q", a.Key, a.Value.String())
			}
		}
	})

	t.Run("zero_numerics", func(t *testing.T) {
		if got := log.BatchSize(0).Value.String(); got != "0" {
			t.Errorf("BatchSize(0) = %q, want \"0\"", got)
		}
		if got := log.ElapsedSeconds(0).Value.String(); got != "0" {
			t.Errorf("ElapsedSeconds(0) = %q, want \"0\"", got)
		}
	})
}

func TestAttrKeysAreStable(t *testing.T) {
	// Every key constant must not change after release.  Assert the canonical
	// wire values so a refactor that alters them is caught.

	keyWire := map[string]string{
		log.KeyError:          "error",
		log.KeyTenantID:       "tenant_id",
		log.KeyRepoPath:       "repo_path",
		log.KeyQueue:          "queue",
		log.KeyIntentID:       "intent_id",
		log.KeyWorkerID:       "worker_id",
		log.KeyComponent:      "component",
		log.KeyRuntimeRole:    "runtime_role",
		log.KeyRepositoryID:   "repository_id",
		log.KeyWorkloadID:     "workload_id",
		log.KeyClusterID:      "cluster_id",
		log.KeyElapsedSeconds: "elapsed_seconds",
		log.KeyBatchSize:      "batch_size",
		log.KeySkipReason:     "skip_reason",
		log.KeyLanguage:       "language",
		log.KeyProvider:       "provider",
		log.KeyOperation:      "operation",
		log.KeyStatus:         "status",
		log.KeyEventKind:      "event_kind",
		log.KeyEventName:      "event_name",
	}

	for key, wire := range keyWire {
		if key != wire {
			t.Errorf("const %q wire value is %q, want %q (const and wire must match)", key, key, wire)
		}
	}
}
