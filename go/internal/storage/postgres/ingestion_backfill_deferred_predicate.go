// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// This file holds the relationship-family candidate predicate: the SQL that
// decides which content/file/gcp_cloud_relationship facts the deferred
// corpus-wide backfill loads before scanning payloads. The predicate is
// deliberately the EXACT predicate migration 059
// (059_relationship_family_candidate_index.sql) uses for its partial index, so
// PostgreSQL can prove the index applies before detoasting generic content
// bodies; TestRelationshipFamilyCandidateIndexMigration pins that lockstep.
// The predicate constants are consumed by
// listDeferredScopedRelationshipFactRecordsQuery
// (ingestion_backfill_deferred_facts.go).

const deferredRelationshipFamilyPathSQL = `lower(COALESCE(
            fact.payload->>'relative_path',
            fact.payload->>'content_path',
            fact.payload->>'file_path',
            fact.payload->>'path',
            ''
          ))`

const deferredRelationshipFamilyArtifactTypeSQL = `lower(COALESCE(fact.payload->>'artifact_type', ''))`

const deferredRelationshipFamilyArgoCDContentMarkerSQL = `(CASE
            WHEN ` + deferredRelationshipFamilyArtifactTypeSQL + ` = 'argocd'
              OR ` + deferredRelationshipFamilyPathSQL + ` ~ '\.ya?ml$'
            THEN lower(COALESCE(
              fact.payload->>'content',
              fact.payload->>'content_body',
              ''
            )) LIKE '%kind: application%'
              OR lower(COALESCE(
                fact.payload->>'content',
                fact.payload->>'content_body',
                ''
              )) LIKE '%kind: applicationset%'
            ELSE false
          END)`

const deferredRelationshipFamilySaltGitfsContentMarkerSQL = `(CASE
            WHEN ` + deferredRelationshipFamilyPathSQL + ` ~ '\.ya?ml$'
              AND COALESCE(
              fact.payload->>'content',
              fact.payload->>'content_body',
              ''
            ) LIKE '%gitfs_remotes:%'
            THEN COALESCE(
              fact.payload->>'content',
              fact.payload->>'content_body',
              ''
            ) ~ E'(^|\n)gitfs_remotes[[:space:]]*:'
            ELSE false
          END)`

// deferredRelationshipFamilyFluxGitRepositoryMarkerSQL admits a "file" fact
// carrying a Flux GitRepository the cross-repo resolver
// (relationships.discoverStructuredFluxEvidence, issue #5483 C2) consumes. That
// resolver reads parsed_file_data.flux_git_repositories[].url, but a "file"
// fact carries NO artifact_type and NO content/content_body (see
// collector/git_fact_builder.go fileFactEnvelope), and a Flux GitRepository
// manifest can live under ANY path — so none of the artifact_type, path, or
// content-marker arms admit it, and before this arm the deferred corpus-wide
// backfill silently dropped every Flux GitRepository file fact. That is the
// source-before-target under-linking gap: a manifest committed before its
// target repo is indexed (or an existing repo's remote_url change) never
// re-resolved, because the deferred re-discovery could not load the manifest
// fact.
//
// This mirrors the ArgoCD precise-signal idiom (loadArgoCDBearingPartitions,
// ingestion_backfill_partition_memo_gate.go): the parser serializes an EMPTY
// flux_git_repositories: [] into every YAML file's parsed_file_data, so a bare
// key-presence test would admit every YAML file. The jsonb_typeof array guard
// plus a non-empty length check admits ONLY a file whose parser actually
// captured a GitRepository, never the empty struct key. Content and
// gcp_cloud_relationship facts have no parsed_file_data key, so the subscript
// is NULL and the arm is false for them.
const deferredRelationshipFamilyFluxGitRepositoryMarkerSQL = `(
            jsonb_typeof(fact.payload -> 'parsed_file_data' -> 'flux_git_repositories') = 'array'
            AND jsonb_array_length(fact.payload -> 'parsed_file_data' -> 'flux_git_repositories') > 0
          )`

const deferredRelationshipFamilyCandidatePredicateSQL = `(
          fact.fact_kind = 'gcp_cloud_relationship'
          OR ` + deferredRelationshipFamilyArtifactTypeSQL + ` IN (
            'terraform',
            'terraform_hcl',
            'terraform_template_text',
            'terragrunt',
            'helm',
            'argocd',
            'dockerfile',
            'docker_compose',
            'github_actions_workflow'
          )
          OR ` + deferredRelationshipFamilyArtifactTypeSQL + ` LIKE 'ansible_%'
          OR ` + deferredRelationshipFamilyPathSQL + ` ~ '(^|/)(dockerfile(\.[^/]*)?|jenkinsfile(\.[^/]*)?|puppetfile|berksfile)$|(^|/)docker-compose\.ya?ml$|(^|/)compose\.ya?ml$|(^|/)\.github/workflows/[^/]+\.ya?ml$|(^|/)applicationsets?/.*\.ya?ml$|(^|/)argocd/.*\.ya?ml$|(^|/)values([^/]*)\.ya?ml$|(^|/)chart\.ya?ml$|(^|/)kustomization\.ya?ml$|(^|/)(playbooks|roles|group_vars|host_vars|inventories)/|(^|/)inventory($|/)|\.(tf|tf\.json|tfvars|tfvars\.json|hcl|tpl)$'
          OR ` + deferredRelationshipFamilyArgoCDContentMarkerSQL + `
          OR ` + deferredRelationshipFamilySaltGitfsContentMarkerSQL + `
          OR ` + deferredRelationshipFamilyFluxGitRepositoryMarkerSQL + `
        )`

const deferredRelationshipFamilyPayloadFactsFilterSQL = `WHERE ` + deferredRelationshipFamilyCandidatePredicateSQL
