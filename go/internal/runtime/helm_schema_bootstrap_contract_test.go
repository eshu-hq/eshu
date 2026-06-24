// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import "testing"

func TestHelmSchemaBootstrapKeepsFailedJobEvidence(t *testing.T) {
	t.Parallel()

	job := requireHelmManifest(t, renderHelmChart(t), "Job", "eshu-schema-bootstrap")
	spec := helmMap(job["spec"])
	if got, want := schemaBootstrapInt(spec["ttlSecondsAfterFinished"]), 86400; got != want {
		t.Fatalf("ttlSecondsAfterFinished = %d, want %d", got, want)
	}

	container := requireHelmContainer(t, job, "schema-bootstrap")
	if got, want := helmString(container["terminationMessagePolicy"]), "FallbackToLogsOnError"; got != want {
		t.Fatalf("terminationMessagePolicy = %q, want %q", got, want)
	}
}

func schemaBootstrapInt(raw any) int {
	switch value := raw.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}
