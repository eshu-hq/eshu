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
