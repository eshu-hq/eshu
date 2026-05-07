package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestLocalHostIngesterOverridesTuneAuthoritativeNornicDBToHostCPU(t *testing.T) {
	originalNumCPU := localHostNumCPU
	t.Cleanup(func() {
		localHostNumCPU = originalNumCPU
	})
	localHostNumCPU = func() int { return 12 }

	got := localHostIngesterOverrides(
		eshulocal.Layout{WorkspaceRoot: "/workspace/repo", CacheDir: "/workspace/cache"},
		localHostModeWatch,
		localHostRuntimeConfig{Profile: query.ProfileLocalAuthoritative, GraphBackend: query.GraphBackendNornicDB},
		func(string) string { return "" },
	)

	if got["ESHU_SNAPSHOT_WORKERS"] != "12" {
		t.Fatalf("ESHU_SNAPSHOT_WORKERS = %q, want 12", got["ESHU_SNAPSHOT_WORKERS"])
	}
	if got["ESHU_PARSE_WORKERS"] != "12" {
		t.Fatalf("ESHU_PARSE_WORKERS = %q, want 12", got["ESHU_PARSE_WORKERS"])
	}
	if got["ESHU_PROJECTOR_WORKERS"] != "12" {
		t.Fatalf("ESHU_PROJECTOR_WORKERS = %q, want 12", got["ESHU_PROJECTOR_WORKERS"])
	}
}

func TestLocalHostReducerOverridesTuneAuthoritativeNornicDBToHostCPU(t *testing.T) {
	originalNumCPU := localHostNumCPU
	t.Cleanup(func() {
		localHostNumCPU = originalNumCPU
	})
	localHostNumCPU = func() int { return 12 }

	got := localHostReducerOverrides(
		2,
		localHostRuntimeConfig{Profile: query.ProfileLocalAuthoritative, GraphBackend: query.GraphBackendNornicDB},
		func(string) string { return "" },
	)

	if got[reducerExpectedSourceLocalProjectorsEnv] != "2" {
		t.Fatalf("%s = %q, want 2", reducerExpectedSourceLocalProjectorsEnv, got[reducerExpectedSourceLocalProjectorsEnv])
	}
	if got["ESHU_REDUCER_WORKERS"] != "12" {
		t.Fatalf("ESHU_REDUCER_WORKERS = %q, want 12", got["ESHU_REDUCER_WORKERS"])
	}
}

func TestLocalHostResourceOverridesPreserveExplicitEnvironment(t *testing.T) {
	originalNumCPU := localHostNumCPU
	t.Cleanup(func() {
		localHostNumCPU = originalNumCPU
	})
	localHostNumCPU = func() int { return 12 }

	getenv := func(key string) string {
		switch key {
		case "ESHU_SNAPSHOT_WORKERS":
			return "3"
		case "ESHU_PARSE_WORKERS":
			return "4"
		case "ESHU_REDUCER_WORKERS":
			return "5"
		case "ESHU_PROJECTOR_WORKERS":
			return "6"
		default:
			return ""
		}
	}

	runtimeConfig := localHostRuntimeConfig{Profile: query.ProfileLocalAuthoritative, GraphBackend: query.GraphBackendNornicDB}
	ingester := localHostIngesterOverrides(
		eshulocal.Layout{WorkspaceRoot: "/workspace/repo", CacheDir: "/workspace/cache"},
		localHostModeWatch,
		runtimeConfig,
		getenv,
	)
	reducer := localHostReducerOverrides(0, runtimeConfig, getenv)

	if ingester["ESHU_SNAPSHOT_WORKERS"] != "" {
		t.Fatalf("ESHU_SNAPSHOT_WORKERS override = %q, want unset when user env exists", ingester["ESHU_SNAPSHOT_WORKERS"])
	}
	if ingester["ESHU_PARSE_WORKERS"] != "" {
		t.Fatalf("ESHU_PARSE_WORKERS override = %q, want unset when user env exists", ingester["ESHU_PARSE_WORKERS"])
	}
	if ingester["ESHU_PROJECTOR_WORKERS"] != "" {
		t.Fatalf("ESHU_PROJECTOR_WORKERS override = %q, want unset when user env exists", ingester["ESHU_PROJECTOR_WORKERS"])
	}
	if reducer["ESHU_REDUCER_WORKERS"] != "" {
		t.Fatalf("ESHU_REDUCER_WORKERS override = %q, want unset when user env exists", reducer["ESHU_REDUCER_WORKERS"])
	}
}
