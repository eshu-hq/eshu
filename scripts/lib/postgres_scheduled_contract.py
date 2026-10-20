"""Exact package, fixture and path contracts for scheduled PostgreSQL proofs."""

PACKAGES = {
    "crossplane": "github.com/eshu-hq/eshu/go/internal/storage/postgres/crossplane",
    "infra_inventory": "github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory",
    "disposable_gate": "github.com/eshu-hq/eshu/go/cmd/golden-corpus-gate",
    "disposable_query": "github.com/eshu-hq/eshu/go/internal/query",
    "disposable_storage": "github.com/eshu-hq/eshu/go/internal/storage/postgres",
    "ack_family": "github.com/eshu-hq/eshu/go/internal/storage/postgres",
    "claim": "github.com/eshu-hq/eshu/go/internal/storage/postgres",
    "liveness": "github.com/eshu-hq/eshu/go/internal/storage/postgres",
    "fanout": "github.com/eshu-hq/eshu/go/internal/storage/postgres",
    "search": "github.com/eshu-hq/eshu/go/internal/storage/postgres",
    "readiness": "github.com/eshu-hq/eshu/go/internal/storage/postgres",
    "containerimage": "github.com/eshu-hq/eshu/go/internal/reducer/containerimage",
    "freshness_links_scale": "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links",
    "freshness_links_a": "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links",
    "freshness_links_b": "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
}

DSNS = {
    "crossplane": "ESHU_CROSSPLANE_REDRIVE_PROOF_DSN",
    "infra_inventory": "ESHU_POSTGRES_DSN",
    "ack_family": "ESHU_POSTGRES_TEST_DSN",
    "claim": "ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN",
    "liveness": "ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN",
    "fanout": "ESHU_POSTGRES_DSN",
    "search": "ESHU_POSTGRES_DSN",
    "readiness": "ESHU_REDUCER_FAIRNESS_PROOF_DSN",
    "containerimage": "ESHU_POSTGRES_DSN",
    "freshness_links_scale": "ESHU_POSTGRES_TEST_DSN",
    "freshness_links_a": "ESHU_POSTGRES_TEST_DSN",
    "freshness_links_b": "ESHU_POSTGRES_TEST_DSN"
}

DATABASES = {
    "crossplane": "eshu_crossplane_scheduled",
    "infra_inventory": "eshu_inventory_scheduled",
    "ack_family": "eshu_ack_scheduled",
    "claim": "eshu_claim_scheduled",
    "liveness": "eshu_liveness_scheduled",
    "fanout": "eshu_fanout_scheduled",
    "search": "eshu_search_scheduled",
    "readiness": "eshu_readiness_scheduled",
    "containerimage": "eshu_containerimage_scheduled",
    "freshness_links_scale": "eshu_freshness_scheduled",
    "freshness_links_a": "eshu_freshness_a",
    "freshness_links_b": "eshu_freshness_b"
}

ADMIN_ENVS = {
    "disposable_gate": (
        "ESHU_TEST_DRAIN_RESIDUAL_POSTGRES",
    ),
    "disposable_query": (
        "ESHU_TEST_CONTENT_INDEX_POSTGRES",
        "ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES",
    ),
    "disposable_storage": (
        "ESHU_TEST_CONTENT_INDEX_POSTGRES",
    ),
}


IMAGE = (
    "ghcr.io/eshu-hq/ci-postgres-alpine@sha256:"
    "77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873"
)

PR_PATHS = {
    ".github/workflows/postgres-scheduled-live.yml",
    "docs/internal/evidence/7127-ledger-retention-timing.sh",
    "docs/internal/evidence/7127-link-writer-fixture.sql",
    "docs/internal/evidence/7127-link-writer-scale.py",
    "go/cmd/golden-corpus-gate/testdata/**",
    "go/go.mod",
    "go/go.sum",
    "go/internal/storage/postgres/migrations/001_ingestion_scopes.sql",
    "go/internal/storage/postgres/migrations/002_scope_generations.sql",
    "go/internal/storage/postgres/migrations/003_fact_records.sql",
    "go/internal/storage/postgres/migrations/136_changed_since_link_ledger.sql",
    "go/internal/query/*fixture*test.go",
    "go/internal/query/testdata/**",
    "go/internal/reducer/containerimage/*.go",
    "go/internal/storage/postgres/*fixture*test.go",
    "go/internal/storage/postgres/ack_supersession_assertions_test.go",
    "go/internal/storage/postgres/crossplane/testdata/**",
    "go/internal/storage/postgres/infra/inventory/testdata/**",
    "go/internal/storage/postgres/projector_queue_supersession_live_test.go",
    "go/internal/storage/postgres/queue_observer_test.go",
    "go/internal/storage/postgres/reducer_queue_cross_scope_readiness_gate_coverage_test.go",
    "go/internal/storage/postgres/reducer_queue_domain_fairness_test.go",
    "go/internal/storage/postgres/reducer_queue_supersede_readiness_test.go",
    "go/internal/storage/postgres/testdata/**",
    "go/internal/testutil/postgresproof/**",
    "scripts/ci/go-mod-download-retry.sh",
    "scripts/lib/live_tests_registry.py",
    "scripts/lib/postgres_scheduled_contract.py",
    "scripts/lib/postgres_scheduled_fragments.py",
    "scripts/lib/postgres_scheduled_inventory.d/**",
    "scripts/lib/postgres_scheduled_inventory.json",
    "scripts/lib/postgres_scheduled_manifest.py",
    "scripts/lib/postgres_scheduled_matrix.py",
    "scripts/lib/postgres_scheduled_runner.py",
    "scripts/lib/postgres_scheduled_workflow.py",
    "scripts/lib/reducer_contention_events.py",
    "scripts/lib/verify-live-tests-ledger.py",
    "scripts/test-postgres-scheduled-fragments.py",
    "scripts/test-postgres-scheduled-ledger-policy.py",
    "scripts/test-postgres-scheduled-matrix.py",
    "scripts/test-postgres-scheduled-runner.py",
    "scripts/test-verify-live-tests-ledger.sh",
    "scripts/verify-live-tests-ledger.sh",
    "specs/ci-gates.d/gates-09.yaml",
    "specs/ci-gates.v1.yaml",
    "specs/live-tests.d/**",
    "specs/live-tests.v1.yaml",
}
