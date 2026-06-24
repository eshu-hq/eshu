// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

func TestKubernetesLiveFactKindRegistry(t *testing.T) {
	t.Parallel()

	wantKinds := []string{
		KubernetesPodTemplateFactKind,
		KubernetesRelationshipFactKind,
		KubernetesWarningFactKind,
	}

	gotKinds := KubernetesLiveFactKinds()
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("KubernetesLiveFactKinds() len = %d, want %d: %#v", len(gotKinds), len(wantKinds), gotKinds)
	}
	for i, want := range wantKinds {
		if gotKinds[i] != want {
			t.Fatalf("KubernetesLiveFactKinds()[%d] = %q, want %q", i, gotKinds[i], want)
		}
		version, ok := KubernetesLiveSchemaVersion(want)
		if !ok {
			t.Fatalf("KubernetesLiveSchemaVersion(%q) ok = false, want true", want)
		}
		if version != "1.0.0" {
			t.Fatalf("KubernetesLiveSchemaVersion(%q) = %q, want 1.0.0", want, version)
		}
	}

	if _, ok := KubernetesLiveSchemaVersion("kubernetes_live.unknown"); ok {
		t.Fatalf("KubernetesLiveSchemaVersion(unknown) ok = true, want false")
	}

	gotKinds[0] = "mutated"
	freshKinds := KubernetesLiveFactKinds()
	if freshKinds[0] != KubernetesPodTemplateFactKind {
		t.Fatalf("KubernetesLiveFactKinds() returned mutable backing slice: %#v", freshKinds)
	}
}

func TestKubernetesLiveFactKindValues(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		KubernetesPodTemplateFactKind:  "kubernetes_live.pod_template",
		KubernetesRelationshipFactKind: "kubernetes_live.relationship",
		KubernetesWarningFactKind:      "kubernetes_live.warning",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("fact kind value = %q, want %q", got, want)
		}
	}
}
