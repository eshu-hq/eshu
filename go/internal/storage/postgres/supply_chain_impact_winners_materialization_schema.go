// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Maintainer watermark for the supply-chain impact winners read model (#3389
// Phase 3). See docs/internal/supply-chain-impact-canonical-dedup-materialization-design.md.
//
// The winners table holds one row per active canonical_key, so an empty table is
// ambiguous: it can mean "the maintainer has not populated the read model yet"
// (building) OR "the maintainer reswept to zero active findings" (a legitimate,
// fresh empty result). Row presence therefore cannot be the freshness signal.
//
// This singleton table carries the maintainer's last-resweep watermark and is
// upserted by the same atomic rebuild statement that reconciles the winners
// table, so it survives a zero-row resweep. The impact-findings read probes this
// watermark: a present row means the maintainer has run (fresh or stale by the
// watermark age, regardless of winner count); an absent row means it has never
// run (building).

// supplyChainImpactWinnersMaterializationSchemaSQL mirrors
// schema/data-plane/postgres/034_supply_chain_impact_winners_materialization.sql.
const supplyChainImpactWinnersMaterializationSchemaSQL = `
CREATE TABLE IF NOT EXISTS supply_chain_impact_winners_materialization (
    singleton SMALLINT PRIMARY KEY DEFAULT 1 CHECK (singleton = 1),
    materialized_at TIMESTAMPTZ NOT NULL
);
`

// supplyChainImpactWinnersMaterializationBootstrapDefinition registers the
// winners maintainer-watermark table so it exists in fresh and bootstrapped data
// planes.
func supplyChainImpactWinnersMaterializationBootstrapDefinition() Definition {
	return Definition{
		Name: "supply_chain_impact_winners_materialization",
		Path: "schema/data-plane/postgres/034_supply_chain_impact_winners_materialization.sql",
		SQL:  supplyChainImpactWinnersMaterializationSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(
		bootstrapDefinitions,
		supplyChainImpactWinnersMaterializationBootstrapDefinition(),
	)
}
