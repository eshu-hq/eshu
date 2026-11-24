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
// clone helper and migration 349: a proof clone starts with the finding write stop
// disabled by trigger name, so a rename of the trigger in the migration without the
// helper would leave every seeding proof refused by the stop (and a rename the other
// way would leave the clones armed).
func TestProofCloneDisarmNamesTheMigrationsTrigger(t *testing.T) {
	t.Parallel()

	for _, def := range postgres.BootstrapDefinitions() {
		if path.Base(def.Path) != "349_supply_chain_impact_finding_fact_write_stop.sql" {
			continue
		}
		if !strings.Contains(def.SQL, "CREATE TRIGGER "+postgresproof.FindingWriteStopTrigger+"\n") {
			t.Fatalf("migration 349 does not create the trigger %q the proof clone helper disarms", postgresproof.FindingWriteStopTrigger)
		}
		return
	}
	t.Fatal("bootstrap definitions carry no migration 349")
}
