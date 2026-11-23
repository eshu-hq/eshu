// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import "testing"

// TestCVEFanOutLeavesAnInFlightManifestMarkerAloneLive runs the real CVE
// fan-out while a manifest marker is claimed (#10972). The marker loads no
// evidence and writes no finding, so the fan-out must not set its replay bit:
// the migration 093 trigger would turn that bit into a pending re-run at the
// marker's ack, the self-loop that kept markers running between bulk events.
// A claimed findings pass in another scope is still flagged, so the exclusion
// is the marker's, not the in-flight read's.
func TestCVEFanOutLeavesAnInFlightManifestMarkerAloneLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	const (
		marker   = "scope:10972:marker"
		findings = "scope:10972:findings"
	)
	ids := map[string]string{}
	for _, scope := range []string{marker, findings} {
		fanOutSeedScope(t, c.ctx, c.db, scope, "g1")
		ids[scope] = fanOutSeedItem(t, c.ctx, c.db, scope, "g1")
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items
SET payload = payload || jsonb_build_object('trigger', 'manifest_dependency') WHERE work_item_id = $1`, ids[marker]); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET status = 'claimed', lease_owner = 'w',
claim_until = now() + interval '1 hour', last_attempt_at = now() WHERE work_item_id = ANY($1)`,
		[]string{ids[marker], ids[findings]}); err != nil {
		t.Fatal(err)
	}

	c.rejectSource()

	if st := fanOutItem(t, c.ctx, c.db, ids[marker]); st.status != "claimed" || st.flag || st.reopened {
		t.Fatalf("in-flight manifest marker = %+v, want claimed and never flagged", st)
	}
	if st := fanOutItem(t, c.ctx, c.db, ids[findings]); st.status != "claimed" || !st.flag {
		t.Fatalf("in-flight findings pass = %+v, want claimed with the replay bit", st)
	}
}
