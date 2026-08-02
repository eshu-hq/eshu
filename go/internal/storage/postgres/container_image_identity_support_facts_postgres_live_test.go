// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestContainerImageIdentityCurrentSupportFactsPreserveGrainPostgresLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the support-grain query proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const (
		scopeID      = "repository:v3-support-grain-live"
		generationID = "generation:v3-support-grain-live"
		supportCount = 513
	)
	digest := "sha256:5740574057405740574057405740574057405740574057405740574057405740"
	cleanupContainerImageIdentitySupportFactsLive(t, ctx, db, scopeID)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupContainerImageIdentitySupportFactsLive(t, cleanupCtx, db, scopeID)
	})
	seedContainerImageIdentitySupportFactsLive(t, ctx, db, scopeID, generationID, digest, supportCount)

	store := NewFactStore(SQLDB{DB: db})
	ciRows, err := store.ListActiveCICDRunCorrelationFacts(ctx, []string{digest}, nil)
	if err != nil {
		t.Fatalf("list CI/CD support facts: %v", err)
	}
	assertContainerImageIdentitySupportRows(t, ciRows, digest, supportCount)

	sbomRows, err := store.ListActiveSBOMAttestationAttachmentFacts(ctx, []string{digest})
	if err != nil {
		t.Fatalf("list SBOM support facts: %v", err)
	}
	assertContainerImageIdentitySupportRows(t, sbomRows, digest, supportCount)

	impactRows, truncated, err := store.ListActiveSupplyChainImpactFacts(
		ctx,
		reducer.SupplyChainImpactFactFilter{SubjectDigests: []string{digest}},
	)
	if err != nil {
		t.Fatalf("list supply-chain support facts: %v", err)
	}
	if truncated {
		t.Fatal("supply-chain support load unexpectedly reported suppression truncation")
	}
	assertContainerImageIdentitySupportRows(t, impactRows, digest, supportCount)

	var aggregateCount int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM container_image_identity_current_facts_for(
    ARRAY[$1]::text[], '{}'::text[], '{}'::text[], '{}'::text[], '{}'::text[], '', 10
)
`, digest).Scan(&aggregateCount); err != nil {
		t.Fatalf("count public digest identities: %v", err)
	}
	if aggregateCount != 1 {
		t.Fatalf("public digest identities = %d, want exactly 1", aggregateCount)
	}
	for _, test := range []struct {
		name      string
		cursor    string
		wantCount int
	}{
		{name: "foreign cursor before support namespace", cursor: "other_fact:before", wantCount: 500},
		{name: "foreign cursor after support namespace", cursor: "z_other_fact:after", wantCount: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var count int
			if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM container_image_identity_current_support_facts_for(
    ARRAY[$1]::text[], '{}'::text[], '{}'::text[], '{}'::text[], '{}'::text[], $2, 500
)
`, digest, test.cursor).Scan(&count); err != nil {
				t.Fatalf("load after foreign cursor %q: %v", test.cursor, err)
			}
			if count != test.wantCount {
				t.Fatalf("support rows after foreign cursor %q = %d, want %d", test.cursor, count, test.wantCount)
			}
		})
	}
	for _, test := range []struct {
		name      string
		cursor    string
		wantCount int
	}{
		{
			name:   "non-hex support cursor",
			cursor: "reducer_container_image_identity_support:zz:zz:zz",
		},
		{
			name:   "non-UTF8 support cursor",
			cursor: "reducer_container_image_identity_support:ff:ff:ff",
		},
		{
			name:   "truncated support cursor",
			cursor: "reducer_container_image_identity_support:zz",
		},
		{
			name:      "short valid hex support cursor",
			cursor:    "reducer_container_image_identity_support:61:62:aa",
			wantCount: 500,
		},
		{
			name:      "non-hex support id after lexical prefix",
			cursor:    "reducer_container_image_identity_support:61:62:zz",
			wantCount: 500,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var count int
			if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM container_image_identity_current_support_facts_for(
    ARRAY[$1]::text[], '{}'::text[], '{}'::text[], '{}'::text[], '{}'::text[], $2, 500
)
`, digest, test.cursor).Scan(&count); err != nil {
				t.Fatalf("load after malformed same-prefix cursor %q: %v", test.cursor, err)
			}
			if count != test.wantCount {
				t.Fatalf("support rows after malformed same-prefix cursor %q = %d, want %d from lexical foreign-cursor ordering", test.cursor, count, test.wantCount)
			}
		})
	}
}

func TestContainerImageIdentityCurrentSupportFactsLegacyCutoverParityPostgresLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the legacy support-grain parity proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const (
		scopeID      = "repository:v3-support-legacy-live"
		generationID = "generation:v3-support-legacy-live"
	)
	digest := "sha256:5740aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	imageRef := "registry.example.com/team/legacy@" + digest
	cleanupContainerImageIdentitySupportFactsLive(t, ctx, db, scopeID)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupContainerImageIdentitySupportFactsLive(t, cleanupCtx, db, scopeID)
	})
	seedContainerImageIdentityHeldSupportScope(t, db, scopeID, generationID)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES (
    'legacy:v3-support-parity-live', $1, $2, 'reducer_container_image_identity',
    'container_image_identity:legacy-parity', 'git', 'intent:legacy-parity',
    clock_timestamp(), clock_timestamp(), jsonb_build_object(
        'digest', $3::text, 'image_ref', $4::text,
        'repository_id', 'registry.example.com/team/legacy',
        'outcome', 'exact_digest', 'identity_strength', 'digest',
        'canonical_writes', 1,
        'source_repository_ids', jsonb_build_array('repository:legacy')
    )
)
`, scopeID, generationID, digest, imageRef); err != nil {
		t.Fatalf("seed legacy support fact: %v", err)
	}

	store := NewFactStore(SQLDB{DB: db})
	legacyRows, err := store.ListActiveCICDRunCorrelationFacts(ctx, []string{digest}, nil)
	if err != nil {
		t.Fatalf("load pre-pointer support: %v", err)
	}
	if len(legacyRows) != 1 || stringPayloadValue(legacyRows[0].Payload, "identity_format") != "digest_v2" {
		t.Fatalf("pre-pointer rows = %#v, want one digest_v2 support", legacyRows)
	}

	if _, err := db.ExecContext(ctx, `
WITH inserted_set AS (
    INSERT INTO container_image_identity_support_sets (
        set_id, scope_id, content_hash, support_count
    ) VALUES (
        decode(repeat('5c', 32), 'hex'), $1, decode(repeat('4c', 32), 'hex'), 1
    ) RETURNING set_id
)
INSERT INTO container_image_identity_supports (
    set_id, digest, support_id, image_ref, repository_id, outcome,
    identity_strength, canonical_writes, source_repository_ids
)
SELECT set_id, $2, decode(repeat('3c', 32), 'hex'), $3,
       'registry.example.com/team/legacy', 'exact_digest', 'digest', 1,
       ARRAY['repository:legacy']::text[]
FROM inserted_set;
`, scopeID, digest, imageRef); err != nil {
		t.Fatalf("insert typed support parity row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE container_image_identity_scope_state
SET active_set_id = decode(repeat('5c', 32), 'hex'),
    last_set_id = decode(repeat('5c', 32), 'hex'),
    last_set_hash = decode(repeat('4c', 32), 'hex'),
    observed_at = clock_timestamp(),
    ingested_at = clock_timestamp()
WHERE scope_id = $1 AND active_generation_id = $2
`, scopeID, generationID); err != nil {
		t.Fatalf("activate typed support parity row: %v", err)
	}
	typedRows, err := store.ListActiveCICDRunCorrelationFacts(ctx, []string{digest}, nil)
	if err != nil {
		t.Fatalf("load post-pointer support: %v", err)
	}
	if len(typedRows) != 1 || stringPayloadValue(typedRows[0].Payload, "identity_format") != "digest_v3" {
		t.Fatalf("post-pointer rows = %#v, want one digest_v3 support", typedRows)
	}
	for _, key := range []string{"digest", "image_ref", "repository_id", "outcome", "identity_strength"} {
		if before, after := stringPayloadValue(legacyRows[0].Payload, key), stringPayloadValue(typedRows[0].Payload, key); before != after {
			t.Fatalf("cutover payload %s changed from %q to %q", key, before, after)
		}
	}
	if before, after := stringSlicePayloadValue(legacyRows[0].Payload, "source_repository_ids"), stringSlicePayloadValue(typedRows[0].Payload, "source_repository_ids"); strings.Join(before, ",") != strings.Join(after, ",") {
		t.Fatalf("cutover source repositories changed from %#v to %#v", before, after)
	}
}

func assertContainerImageIdentitySupportRows(
	t *testing.T,
	rows []facts.Envelope,
	digest string,
	wantCount int,
) {
	t.Helper()
	if len(rows) != wantCount {
		t.Fatalf("support rows = %d, want %d", len(rows), wantCount)
	}
	seenFactIDs := make(map[string]struct{}, wantCount)
	seenImageRefs := make(map[string]struct{}, wantCount)
	var singletonSupports, ambiguousSupports, buildSupports int
	for _, row := range rows {
		if row.FactKind != "reducer_container_image_identity" {
			t.Fatalf("fact kind = %q, want reducer_container_image_identity", row.FactKind)
		}
		if !strings.HasPrefix(row.FactID, "reducer_container_image_identity_support:") {
			t.Fatalf("support fact ID %q lacks the support-grain namespace", row.FactID)
		}
		if _, duplicate := seenFactIDs[row.FactID]; duplicate {
			t.Fatalf("duplicate support fact ID %q across paged results", row.FactID)
		}
		seenFactIDs[row.FactID] = struct{}{}
		if got := stringPayloadValue(row.Payload, "digest"); got != digest {
			t.Fatalf("support digest = %q, want %q", got, digest)
		}
		if got, want := stringPayloadValue(row.Payload, "canonical_id"), "canonical:container_image_identity:"+digest; got != want {
			t.Fatalf("canonical ID = %q, want %q", got, want)
		}
		if got := stringPayloadValue(row.Payload, "identity_format"); got != "digest_v3" {
			t.Fatalf("identity format = %q, want digest_v3", got)
		}
		imageRef := stringPayloadValue(row.Payload, "image_ref")
		if _, duplicate := seenImageRefs[imageRef]; duplicate {
			t.Fatalf("duplicate correlated image_ref %q", imageRef)
		}
		seenImageRefs[imageRef] = struct{}{}
		sourceRepositories := stringSlicePayloadValue(row.Payload, "source_repository_ids")
		switch len(sourceRepositories) {
		case 1:
			singletonSupports++
		case 2:
			ambiguousSupports++
		default:
			t.Fatalf("source repositories for %q = %#v, want one correlated singleton or one two-repository ambiguity", row.FactID, sourceRepositories)
		}
		if len(stringSlicePayloadValue(row.Payload, "build_provenance_repository_ids")) > 0 {
			buildSupports++
		}
	}
	if singletonSupports != wantCount-1 || ambiguousSupports != 1 || buildSupports != 1 {
		t.Fatalf(
			"support correlation counts = singleton:%d ambiguous:%d build:%d, want %d/1/1",
			singletonSupports, ambiguousSupports, buildSupports, wantCount-1,
		)
	}
}

func stringPayloadValue(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func stringSlicePayloadValue(payload map[string]any, key string) []string {
	values, _ := payload[key].([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func seedContainerImageIdentitySupportFactsLive(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	digest string,
	supportCount int,
) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin support-grain seed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	statements := []struct {
		query string
		args  []any
	}{
		{query: `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', 'git', $1, 'git', $1, clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)
`, args: []any{scopeID}},
		{query: `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES ($2, $1, 'test', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)
`, args: []any{scopeID, generationID}},
		{query: `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, args: []any{scopeID, generationID}},
		{query: `
INSERT INTO container_image_identity_support_sets (set_id, scope_id, content_hash, support_count)

VALUES (decode(repeat('57', 32), 'hex'), $1, decode(repeat('40', 32), 'hex'), $2)
`, args: []any{scopeID, supportCount}},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed support-grain scope: %v", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO container_image_identity_supports (
    set_id, digest, support_id, image_ref, repository_id, outcome,
    identity_strength, canonical_writes, source_repository_ids,
    build_provenance_repository_ids, source_layers
)
SELECT
    decode(repeat('57', 32), 'hex'),
    $1::text,
    sha256(convert_to(format('support-%06s', n), 'UTF8')),
    format('registry.example.com/team/app-%06s@%s', n, $1::text),
    format('registry.example.com/team/app-%06s', n),
    'exact_digest',
    'digest',
    1,
    CASE WHEN n = 2
        THEN ARRAY['repository:builder', 'repository:deploy']::text[]
        ELSE ARRAY['repository:deploy']::text[]
    END,
    CASE WHEN n = 2
        THEN ARRAY['repository:builder']::text[]
        ELSE '{}'::text[]
    END,
    ARRAY['observed_resource', 'source_declaration']::text[]
FROM generate_series($2::integer, 1, -1) AS n
`, digest, supportCount); err != nil {
		t.Fatalf("seed support-grain rows: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE container_image_identity_scope_state
SET active_set_id = decode(repeat('57', 32), 'hex'),
    last_set_id = decode(repeat('57', 32), 'hex'),
    last_set_hash = decode(repeat('40', 32), 'hex'),
    source_system = 'git', collector_kind = 'git', source_confidence = 'inferred',
    source_fact_key = 'intent:v3-support-grain-live',
    observed_at = clock_timestamp(), ingested_at = clock_timestamp()
WHERE scope_id = $1 AND active_generation_id = $2
`, scopeID, generationID); err != nil {
		t.Fatalf("activate support-grain set: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit support-grain seed: %v", err)
	}
}

func cleanupContainerImageIdentitySupportFactsLive(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID); err != nil {
		t.Fatalf("clean support-grain scope: %v", err)
	}
}
