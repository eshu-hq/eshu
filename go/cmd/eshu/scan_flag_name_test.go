// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

// TestScanWaitFlagIsRegisteredWhereItIsPrinted pins the one flag name the
// scan family declares, scan.WaitFlag, to the subcommands this binary
// registers it on. internal/cli/vulnscan prints "rerun with --wait=true" when
// a scan stops short of ready, and cannot import go/cmd/eshu to check the
// flag exists; the same inversion component_flag_name_test.go covers applies
// here -- the package that prints the name declares it, the registration
// consumes the declaration, and this test covers the half a compiler cannot:
// that the flag is still registered, from that constant, on the subcommand
// whose run prints the message (`vuln-scan repo`) and on the command whose
// family owns the flag (`scan`, which shares addScanFlags). Re-introducing a
// literal "wait" at a registration site and then renaming the constant's value
// lands here as a missing flag; a literal alone is invisible to this test and
// is caught only when the two strings later diverge, which is why the
// registration reads the constant.
//
// Resolves paths against the shared rootCmd tree, so it takes
// lockCommandTree; see command_tree_test.go.
func TestScanWaitFlagIsRegisteredWhereItIsPrinted(t *testing.T) {
	lockCommandTree(t)

	cases := []struct {
		name string
		path []string
	}{
		{name: "vuln-scan repo prints the message", path: []string{"vuln-scan", "repo"}},
		{name: "scan owns the flag", path: []string{"scan"}},
	}
	if len(cases) != 2 {
		t.Fatalf("registration coverage names %d subcommands, want 2; move this number only alongside the registration you added or removed", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _, err := rootCmd.Find(tc.path)
			if err != nil {
				t.Fatalf("rootCmd.Find(%v) error = %v, want nil", tc.path, err)
			}
			if cmd.Flags().Lookup(scan.WaitFlag) == nil {
				t.Fatalf("%v does not register --%s; internal/cli/vulnscan prints \"rerun with --%s=true\", so an operator following that message has no such flag", tc.path, scan.WaitFlag, scan.WaitFlag)
			}
		})
	}
}
