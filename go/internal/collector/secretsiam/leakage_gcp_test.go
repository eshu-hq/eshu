// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestGCPBuildersFingerprintRawSensitiveValues(t *testing.T) {
	t.Parallel()

	cases := secretsIAMBuilderCases(t)
	checks := map[string][]string{
		"gcp_trust_policy": {
			"app@demo-proj.iam.gserviceaccount.com",
			"demo-proj.svc.id.goog",
			"ns-canary",
			"ksa-canary",
		},
		"k8s_gcp_workload_identity_binding": {
			"app@demo-proj.iam.gserviceaccount.com",
			"demo-proj.svc.id.goog",
			"ns-canary",
			"ksa-canary",
		},
	}
	for name, raws := range checks {
		for _, raw := range raws {
			assertPayloadRawValueAbsent(t, name, cases[name], raw)
		}
	}
}

func assertPayloadRawValueAbsent(t *testing.T, name string, env facts.Envelope, raw string) {
	t.Helper()

	for key, val := range env.Payload {
		if leakValueContains(val, raw) {
			t.Fatalf("%s: payload[%q] leaks raw value %q (should be fingerprinted)", name, key, raw)
		}
	}
}
