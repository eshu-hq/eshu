// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestHandlesRouteEndpointPresenceGateEnabledDefaultsOn verifies the
// handles_route endpoint-presence gate (#2809) is wired by default — so
// Function-[:HANDLES_ROUTE]->Endpoint edges are gated on their target endpoint
// committing in every reducer deployment, not only when secrets-IAM graph
// projection is enabled — and that it can be explicitly disabled.
func TestHandlesRouteEndpointPresenceGateEnabledDefaultsOn(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "unset defaults on", env: map[string]string{}, want: true},
		{name: "explicit true", env: map[string]string{handlesRouteEndpointPresenceGateEnabledEnv: "true"}, want: true},
		{name: "explicit false disables", env: map[string]string{handlesRouteEndpointPresenceGateEnabledEnv: "false"}, want: false},
		{name: "explicit 0 disables", env: map[string]string{handlesRouteEndpointPresenceGateEnabledEnv: "0"}, want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(k string) string { return tc.env[k] }
			if got := handlesRouteEndpointPresenceGateEnabled(getenv); got != tc.want {
				t.Fatalf("handlesRouteEndpointPresenceGateEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEndpointPresenceWiringNilWhenDisabled confirms a disabled gate yields the
// nil writer/lookup pair that keeps every materializer and gate byte-identical.
func TestEndpointPresenceWiringNilWhenDisabled(t *testing.T) {
	writer, lookup := endpointPresenceWiring(false, nil)
	if writer != nil || lookup != nil {
		t.Fatalf("endpointPresenceWiring(false) = (%v, %v), want (nil, nil)", writer, lookup)
	}
}

// TestNewEndpointPresenceWiringsGatesIndependently proves the two presence
// concerns never couple (#2809 review): the secrets/IAM uid pair (#1380) is
// driven solely by the secrets/IAM enable flag, and the handles_route
// (repo_id, path) pair solely by its own kill switch. Each combination must
// leave the other pair untouched.
func TestNewEndpointPresenceWiringsGatesIndependently(t *testing.T) {
	cases := []struct {
		name              string
		secretsIAMEnabled bool
		handlesRouteEnv   map[string]string
		wantSecretsIAM    bool
		wantHandlesRoute  bool
	}{
		{
			name:              "default: secrets/IAM off, handles_route on",
			secretsIAMEnabled: false,
			handlesRouteEnv:   map[string]string{},
			wantSecretsIAM:    false,
			wantHandlesRoute:  true,
		},
		{
			name:              "secrets/IAM on does not depend on handles_route",
			secretsIAMEnabled: true,
			handlesRouteEnv:   map[string]string{handlesRouteEndpointPresenceGateEnabledEnv: "false"},
			wantSecretsIAM:    true,
			wantHandlesRoute:  false,
		},
		{
			name:              "handles_route kill switch leaves secrets/IAM off",
			secretsIAMEnabled: false,
			handlesRouteEnv:   map[string]string{handlesRouteEndpointPresenceGateEnabledEnv: "false"},
			wantSecretsIAM:    false,
			wantHandlesRoute:  false,
		},
		{
			name:              "both enabled",
			secretsIAMEnabled: true,
			handlesRouteEnv:   map[string]string{},
			wantSecretsIAM:    true,
			wantHandlesRoute:  true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(k string) string { return tc.handlesRouteEnv[k] }
			wirings := newEndpointPresenceWirings(getenv, tc.secretsIAMEnabled, nil)

			gotSecretsIAM := wirings.secretsIAMWriter != nil && wirings.secretsIAMLookup != nil
			if gotSecretsIAM != tc.wantSecretsIAM {
				t.Fatalf("secrets/IAM pair wired = %v, want %v", gotSecretsIAM, tc.wantSecretsIAM)
			}
			gotHandlesRoute := wirings.handlesRouteWriter != nil && wirings.handlesRouteLookup != nil
			if gotHandlesRoute != tc.wantHandlesRoute {
				t.Fatalf("handles_route pair wired = %v, want %v", gotHandlesRoute, tc.wantHandlesRoute)
			}
		})
	}
}
