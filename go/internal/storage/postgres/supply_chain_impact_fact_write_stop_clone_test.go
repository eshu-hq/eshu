// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"path"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestProofCloneDisarmNamesTheMigrationsTrigger pins the coupling between the proof
// helpers and migration 358: the scoped and whole-test disarm name the trigger by its
// constant, so a rename of the trigger in the migration without the helper would make
// every disarm a silent no-op and leave every seeding proof refused by the stop.
func TestProofCloneDisarmNamesTheMigrationsTrigger(t *testing.T) {
	t.Parallel()

	for _, def := range postgres.BootstrapDefinitions() {
		if path.Base(def.Path) != "358_supply_chain_impact_finding_fact_write_stop.sql" {
			continue
		}
		if !strings.Contains(def.SQL, "CREATE TRIGGER "+postgresproof.FindingWriteStopTrigger+"\n") {
			t.Fatalf("migration 358 does not create the trigger %q the proof clone helper disarms", postgresproof.FindingWriteStopTrigger)
		}
		return
	}
	t.Fatal("bootstrap definitions carry no migration 358")
}
