// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	kubernetestools "github.com/eshu-hq/eshu/go/internal/mcp/kubernetes"
)

func TestReadOnlyToolsKeepsKubernetesRegistrationPosition(t *testing.T) {
	t.Parallel()

	wantKubernetes := kubernetestools.Tools()
	if got := kubernetesTools(); !reflect.DeepEqual(got, wantKubernetes) {
		t.Fatal("root kubernetesTools wrapper drifted from kubernetes.Tools")
	}

	tools := ReadOnlyTools()
	for start := range tools {
		if tools[start].Name != "list_codeowners_ownership" {
			continue
		}
		kubernetesStart := start + 1
		secretsStart := kubernetesStart + len(wantKubernetes)
		if secretsStart >= len(tools) {
			break
		}
		if got := tools[secretsStart].Name; got != "list_secrets_iam_identity_trust_chains" {
			break
		}
		if got := tools[kubernetesStart:secretsStart]; !reflect.DeepEqual(got, wantKubernetes) {
			t.Fatal("ReadOnlyTools kubernetes definitions drifted from kubernetes.Tools")
		}
		return
	}
	t.Fatal("ReadOnlyTools missing ordered codeowners/kubernetes/secrets boundary")
}
