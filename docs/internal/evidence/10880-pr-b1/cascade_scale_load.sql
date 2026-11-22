-- Loads the cascade shim: same columns and indexes as migration 336 (without the
-- generation_id index, which the probe script adds for the "with" variant), 60
-- supports per generation, one generation per scope.
DROP SCHEMA IF EXISTS shim CASCADE;
CREATE SCHEMA shim;
SET search_path = shim;
CREATE TABLE shim_generations (generation_id TEXT PRIMARY KEY, scope_id TEXT NOT NULL);
CREATE TABLE shim_supports (
    fact_id                TEXT PRIMARY KEY,
    canonical_key          TEXT NOT NULL,
    scope_id               TEXT NOT NULL,
    generation_id          TEXT NOT NULL REFERENCES shim_generations(generation_id) ON DELETE CASCADE,
    finding_id             TEXT NOT NULL,
    has_payload_finding_id SMALLINT NOT NULL,
    priority_score         INTEGER NOT NULL,
    source_confidence      TEXT NOT NULL,
    suppression_state      TEXT NOT NULL,
    impact_status          TEXT NOT NULL DEFAULT '',
    ecosystem              TEXT NOT NULL DEFAULT '',
    severity_bucket        TEXT NOT NULL DEFAULT '',
    repository_id          TEXT NOT NULL DEFAULT '',
    cve_id                 TEXT NOT NULL DEFAULT '',
    advisory_id            TEXT NOT NULL DEFAULT '',
    package_id             TEXT NOT NULL DEFAULT '',
    subject_digest         TEXT NOT NULL DEFAULT '',
    image_ref              TEXT NOT NULL DEFAULT '',
    priority_bucket        TEXT NOT NULL DEFAULT '',
    detection_profile      TEXT NOT NULL DEFAULT '',
    observed_version       TEXT NOT NULL DEFAULT '',
    match_reason           TEXT NOT NULL DEFAULT '',
    payload                JSONB NOT NULL
);
INSERT INTO shim_generations SELECT 'gen:' || n, 'scope:' || n FROM generate_series(1, :gens) AS n;
INSERT INTO shim_supports (fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id,
  priority_score, source_confidence, suppression_state, impact_status, repository_id, cve_id, package_id,
  priority_bucket, severity_bucket, payload)
SELECT 'fact:' || n || ':' || k, 'key:' || n || ':' || k, 'scope:' || n, 'gen:' || n,
       'finding:' || n || ':' || k, 1, (k * 7) % 100, 'observed', 'active', 'affected_exact',
       'repository:' || n, 'CVE-2026-' || k, 'pkg:npm/p' || k, 'high', 'high',
       jsonb_build_object('finding_id', 'finding:' || n || ':' || k, 'impact_status', 'affected_exact',
                          'priority_score', (k * 7) % 100, 'note', repeat('x', 200))
FROM generate_series(1, :gens) AS n, generate_series(1, 60) AS k;
CREATE INDEX shim_scope_generation_idx ON shim_supports (scope_id, generation_id);
CREATE INDEX shim_repository_idx ON shim_supports (repository_id);
CREATE INDEX shim_finding_idx ON shim_supports (finding_id);
CREATE INDEX shim_operator_idx ON shim_supports (canonical_key) WHERE scope_id = 'operator:vulnerability_suppressions';
VACUUM ANALYZE shim_generations;
VACUUM ANALYZE shim_supports;
SELECT pg_size_pretty(pg_relation_size('shim_supports')) AS heap,
       pg_size_pretty(pg_relation_size('shim_scope_generation_idx')) AS scope_generation_idx,
       (SELECT count(*) FROM shim_supports) AS supports;
