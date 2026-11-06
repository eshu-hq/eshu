// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// manifestLiveAlert builds one security_alert.repository_alert fact naming
// packageName in repositoryID.
func manifestLiveAlert(factID, repositoryID, packageName, state string) facts.Envelope {
	return facts.Envelope{
		FactID: factID, FactKind: facts.SecurityAlertRepositoryAlertFactKind, StableFactKey: factID,
		SourceRef: facts.Ref{SourceSystem: "security_alert"},
		Payload: map[string]any{
			"repository_id": repositoryID, "provider": "github", "provider_state": state,
			"ecosystem": "npm", "package_name": packageName,
			"package_id": "npm://registry.npmjs.org/" + packageName,
		},
	}
}

// TestManifestTriggerReopensSecurityAlertHolderLive is the #10061 acceptance
// proof on a real queue: a lockfile delta that adds a lodash pin named by an
// active repository alert reopens that alert scope's succeeded
// supply_chain_impact item. Alert scopes whose alert names another package,
// another repository, a dismissed alert, or only a superseded generation stay
// untouched.
func TestManifestTriggerReopensSecurityAlertHolderLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	at := func(minute int) time.Time { return time.Date(2026, 11, 4, 0, minute, 0, 0, time.UTC) }
	manifestLiveSeedGitScope(t, ctx, db, "gen-2",
		manifestLiveGeneration{id: "gen-1", status: "superseded", commit: "c1", activated: true, ingested: at(1)},
		manifestLiveGeneration{id: "gen-2", status: "active", commit: "c2", baseline: "c1", delta: true, activated: true, ingested: at(2)},
	)
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-1", manifestLiveRepository("repo-1", false))
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-1", manifestLiveDependency("g1-react", "package-lock.json", "react", "18.0.0"))
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-2", manifestLiveRepository("repo-2", true, "package-lock.json"))
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-2", manifestLiveDependency("g2-react", "package-lock.json", "react", "18.0.0"))
	manifestLiveSeedFact(t, ctx, db, manifestLiveGitScope, "gen-2", manifestLiveDependency("g2-lodash", "package-lock.json", "lodash", "4.17.21"))

	const repo = "repository:r_checkout"
	cases := []struct {
		scope string
		// gen is the generation the alert fact is seeded in; the item sits at
		// the scope's active generation.
		active, factGen string
		alert           facts.Envelope
		want            string
	}{
		{"security-alert:github:acme/api", "g1", "g1", manifestLiveAlert("alert-lodash", repo, "lodash", "open"), "pending"},
		{"security-alert:github:acme/other-package", "g1", "g1", manifestLiveAlert("alert-express", repo, "express", "open"), "succeeded"},
		{"security-alert:github:acme/other-repo", "g1", "g1", manifestLiveAlert("alert-elsewhere", "repository:r_other", "lodash", "open"), "succeeded"},
		{"security-alert:github:acme/dismissed", "g1", "g1", manifestLiveAlert("alert-dismissed", repo, "lodash", "dismissed"), "succeeded"},
		{"security-alert:github:acme/superseded", "g2", "g1", manifestLiveAlert("alert-old", repo, "lodash", "open"), "succeeded"},
	}
	for _, tc := range cases {
		if tc.factGen == tc.active {
			fanOutSeedScope(t, ctx, db, tc.scope, tc.active)
		} else {
			fanOutSeedScope(t, ctx, db, tc.scope, tc.active, tc.factGen)
		}
		manifestLiveSeedFact(t, ctx, db, tc.scope, fanOutGen(tc.scope, tc.factGen), tc.alert)
		id := fanOutSeedItem(t, ctx, db, tc.scope, tc.active)
		if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = $1`, id); err != nil {
			t.Fatalf("mark %s succeeded: %v", tc.scope, err)
		}
	}

	handler := supplychaincore.SupplyChainImpactHandler{
		FencingTokenIssuer: manifestLiveNoToken{t: t},
		FactLoader:         manifestLiveNoLoad{t: t},
		Writer:             manifestLiveNoWrite{t: t},
		FanOut:             fanOutLiveStore(db),
	}
	markerID := manifestLiveSeedMarker(t, ctx, db, "gen-2", "pending", false)
	intent, ok, err := fanOutLiveQueue(db).Claim(ctx)
	if err != nil || !ok || intent.IntentID != markerID {
		t.Fatalf("Claim() = %q, %v, %v; want the marker item %q", intent.IntentID, ok, err, markerID)
	}
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	for _, tc := range cases {
		var status string
		if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`,
			fanOutItemID(tc.scope, fanOutGen(tc.scope, tc.active))).Scan(&status); err != nil {
			t.Fatalf("read status %s: %v", tc.scope, err)
		}
		if status != tc.want {
			t.Errorf("%s status = %q, want %q", tc.scope, status, tc.want)
		}
	}
}
