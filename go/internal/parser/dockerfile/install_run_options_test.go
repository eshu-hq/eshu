// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import "testing"

func TestInstallEvidenceRunAndManagerLeadingOptions(t *testing.T) {
	cases := []struct {
		name, run, manager, packageName, reason string
		credentialDrops                         int
	}{
		{"cache mount", "RUN --mount=type=cache,target=/var/cache/apt apt-get install curl", "apt-get", "curl", "", 0},
		{"secret mount identifier", "RUN --mount=type=secret,id=apt apt-get install curl", "apt-get", "curl", "", 0},
		{"combined run options", "RUN --network=none --mount=type=cache,target=/var/cache/apt apt-get install curl", "apt-get", "curl", "", 0},
		{"manager option before action", "RUN apt-get --no-install-recommends install -y gcc", "apt-get", "gcc", "", 0},
		{"manager value option before action", "RUN apt-get -o APT::Get::Assume-Yes=true install gcc", "apt-get", "gcc", "", 0},
		{"unknown run option", "RUN --mystery=value apt-get install curl", "", "", "unsupported_run_option", 0},
		{"unknown manager option", "RUN apt-get --mystery install curl", "", "", "unsupported_option", 0},
		{"credentialed run option", "RUN --mount=type=cache,id=token=exampleSecretToken123 apt-get install curl", "", "", "credential_run_option", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RuntimeMetadata("FROM debian\n" + tc.run + "\n")
			if got.DroppedCredentials != tc.credentialDrops {
				t.Fatalf("credential drops = %d, want %d", got.DroppedCredentials, tc.credentialDrops)
			}
			if tc.reason != "" {
				if len(got.Installs) != 0 || len(got.Unparsed) != 1 || got.Unparsed[0].Reason != tc.reason {
					t.Fatalf("installs = %#v, unparsed = %#v, want reason %q", got.Installs, got.Unparsed, tc.reason)
				}
				return
			}
			if len(got.Installs) != 1 || got.Installs[0].Manager != tc.manager || got.Installs[0].Name != tc.packageName || len(got.Unparsed) != 0 {
				t.Fatalf("installs = %#v, unparsed = %#v, want %s %s", got.Installs, got.Unparsed, tc.manager, tc.packageName)
			}
		})
	}
}
