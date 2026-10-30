// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

// ssmSeedCallsite is the builder identity the coverage gate records for
// both SSM committed-seed legs: they share one Go function
// (readSSMCommittedSeeds issues the apps leg then the platform leg).
const ssmSeedCallsite = "internal/query/ssm_host_inventory_committed.go:readSSMCommittedSeeds"

// ssmSeedAppsStatement and ssmSeedPlatsStatement mirror the production
// seed-leg constants in go/internal/query/ssm_host_inventory_read_model.go
// (whitespace folding is the gate's normalization, so layout need not
// match). Both legs take the same parameters, which is what makes them
// one dispatch family in the coverage evaluation.
const ssmSeedAppsStatement = `MATCH (seed:SSMHostApplication)
WHERE ($all_scopes OR seed.scope_id IN $scope_ids)
  AND ($instance_id = '' OR seed.instance_id = $instance_id)
  AND ($account_id = '' OR seed.account_id = $account_id)
  AND ($region = '' OR seed.region = $region)
RETURN DISTINCT seed.partition AS h_partition, seed.account_id AS h_account_id, seed.region AS h_region, seed.instance_id AS h_instance_id
ORDER BY h_partition, h_account_id, h_region, h_instance_id
LIMIT $seed_limit`

const ssmSeedPlatsStatement = `MATCH (seed:SSMHostPlatform)
WHERE ($all_scopes OR seed.scope_id IN $scope_ids)
  AND ($instance_id = '' OR seed.instance_id = $instance_id)
  AND ($account_id = '' OR seed.account_id = $account_id)
  AND ($region = '' OR seed.region = $region)
RETURN DISTINCT seed.partition AS h_partition, seed.account_id AS h_account_id, seed.region AS h_region, seed.instance_id AS h_instance_id
ORDER BY h_partition, h_account_id, h_region, h_instance_id
LIMIT $seed_limit`

const ssmSeedParams = `{"all_scopes":true,"scope_ids":[],"instance_id":"","account_id":"","region":"","seed_limit":101}`

func ssmSeedRecord(statement string) DifferentialRecord {
	return DifferentialRecord{
		Backend:     "neo4j",
		Callsite:    ssmSeedCallsite,
		Fingerprint: DifferentialFingerprint{Statement: statement, Parameters: ssmSeedParams},
		RowCount:    0,
		Digest:      "empty-digest",
	}
}

// TestStatementCoverageSSMCommittedSeedsExempt pins the B-7 exemption for
// the SSM committed-seed legs (eshu-hq/eshu#9602): the 20-repo corpus
// carries no SSM host fixtures by design, so both legs return zero rows on
// every backend. Without a manifest row the gate fails the shared callsite
// as always-empty (observed on neo4j in the #9602 wave).
func TestStatementCoverageSSMCommittedSeedsExempt(t *testing.T) {
	t.Parallel()

	manifest, err := queryplan.LoadBuilderManifest("../queryplan/testdata/statement-builders.yaml")
	if err != nil {
		t.Fatalf("LoadBuilderManifest() error = %v", err)
	}
	records := map[string][]DifferentialRecord{"neo4j": {
		ssmSeedRecord(ssmSeedAppsStatement),
		ssmSeedRecord(ssmSeedPlatsStatement),
	}}
	// The synthetic record set carries only the seed legs, so unrelated
	// manifest findings (stale exemptions, never-executed builders) also
	// surface here; the assertion scopes to this test's callsite.
	for _, failure := range ComputeStatementCoverage(manifest, records).Failures() {
		if strings.Contains(failure.ID, ssmSeedCallsite) {
			t.Fatalf("Failures() contains %v: the corpus carries no SSM fixtures, so the seed legs are empty by construction", failure)
		}
	}
}
