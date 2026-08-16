// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	clicomponent "github.com/eshu-hq/eshu/go/internal/cli/component"
)

// TestComponentRequiredFlagNamesAreRegistered pins the five flag names
// internal/cli/component prints in its "--<flag> is required" errors to the
// flags this binary actually registers.
//
// The extraction under #6059 moved that validation out of package main, and
// the package cannot import go/cmd/eshu to reach the names. The fix inverts
// ownership: internal/cli/component declares each name it renders, and the
// registrations below consume those declarations, so renaming one is a
// compile error rather than a stale operator-facing message. This test covers
// the half a compiler cannot: that the registration for each name still
// exists, on the subcommand whose body raises that error. Re-introducing a
// literal at a registration site -- the drift the inversion removes -- lands
// here as a missing flag.
//
// Not parallel, and neither are the subtests: cobra's Find writes the resolved
// name back onto the shared rootCmd tree, so two goroutines resolving a path
// at once is a data race -- which -race caught here before this test was ever
// committed. The sibling registration tests in change_impact_test.go run
// serially for the same reason.
func TestComponentRequiredFlagNamesAreRegistered(t *testing.T) {
	cases := []struct {
		name string
		path []string
		flag string
	}{
		{name: "enable instance", path: []string{"component", "enable"}, flag: clicomponent.InstanceFlag},
		{name: "disable instance", path: []string{"component", "disable"}, flag: clicomponent.InstanceFlag},
		{name: "uninstall version", path: []string{"component", "uninstall"}, flag: clicomponent.VersionFlag},
		{name: "init collector id", path: []string{"component", "init", "collector"}, flag: clicomponent.InitIDFlag},
		{name: "init collector publisher", path: []string{"component", "init", "collector"}, flag: clicomponent.InitPublisherFlag},
		{name: "init collector fact kind", path: []string{"component", "init", "collector"}, flag: clicomponent.InitFactKindFlag},
	}
	if len(cases) != 6 {
		t.Fatalf("parity covers %d required flag names, want 6; move this number only alongside the name you added or deleted, and say which one", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, _, err := rootCmd.Find(tc.path)
			if err != nil {
				t.Fatalf("rootCmd.Find(%v) error = %v, want nil", tc.path, err)
			}
			if cmd.Flags().Lookup(tc.flag) == nil {
				t.Fatalf("%v does not register --%s; internal/cli/component renders \"--%s is required\", so an operator following that message has no such flag", tc.path, tc.flag, tc.flag)
			}
		})
	}
}
