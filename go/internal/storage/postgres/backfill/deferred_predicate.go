// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

// This file holds the relationship-family candidate predicate: the SQL that
// decides which content/file/gcp_cloud_relationship facts the deferred
// corpus-wide backfill loads before scanning payloads. The predicate is
// deliberately the EXACT predicate migration 059
// (059_relationship_family_candidate_index.sql) uses for its partial index, so
// PostgreSQL can prove the index applies before detoasting generic content
// bodies; TestRelationshipFamilyCandidateIndexMigration pins that lockstep.
// The predicate constants are consumed by
// ListDeferredScopedRelationshipFactRecordsQuery
// (ingestion_backfill_deferred_facts.go).

// Exported for the root relationship-family guard proof, which pins each
// marker arm inside the shipped candidate predicate (issue #7794 seam).
const DeferredRelationshipFamilyPathSQL = `lower(COALESCE(
            fact.payload->>'relative_path',
            fact.payload->>'content_path',
            fact.payload->>'file_path',
            fact.payload->>'path',
            ''
          ))`

const deferredRelationshipFamilyArtifactTypeSQL = `lower(COALESCE(fact.payload->>'artifact_type', ''))`

const deferredRelationshipFamilyArgoCDContentMarkerSQL = `(CASE
            WHEN ` + deferredRelationshipFamilyArtifactTypeSQL + ` = 'argocd'
              OR ` + DeferredRelationshipFamilyPathSQL + ` ~ '\.ya?ml$'
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

// Exported for the root relationship-family guard proof, which pins each
// marker arm inside the shipped candidate predicate (issue #7794 seam).
const DeferredRelationshipFamilySaltGitfsContentMarkerSQL = `(CASE
            WHEN ` + DeferredRelationshipFamilyPathSQL + ` ~ '\.ya?ml$'
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

// DeferredRelationshipFamilyFluxGitRepositoryMarkerSQL admits a "file" fact
// carrying a Flux GitRepository the cross-repo resolver
// (relationships.discoverStructuredFluxEvidence, issue #5483 C2) consumes. That
// resolver reads parsed_file_data.flux_git_repositories[].url, but a "file"
// fact carries NO artifact_type and NO content/content_body (see
// collector/git/content/envelopes.go gitcontent.FileFactEnvelope), and a Flux GitRepository
// manifest can live under ANY path — so none of the artifact_type, path, or
// content-marker arms admit it, and before this arm the deferred corpus-wide
// backfill silently dropped every Flux GitRepository file fact. That is the
// source-before-target under-linking gap: a manifest committed before its
// target repo is indexed (or an existing repo's remote_url change) never
// re-resolved, because the deferred re-discovery could not load the manifest
// fact.
//
// This mirrors the ArgoCD precise-signal idiom (LoadArgoCDBearingPartitions,
// ingestion_backfill_partition_memo_gate.go): the parser serializes an EMPTY
// flux_git_repositories: [] into every YAML file's parsed_file_data, so a bare
// key-presence test would admit every YAML file. The jsonb_typeof array guard
// plus a non-empty length check admits ONLY a file whose parser actually
// captured a GitRepository, never the empty struct key. Content and
// gcp_cloud_relationship facts have no parsed_file_data key, so the subscript
// is NULL and the arm is false for them.
// Exported for the root relationship-family guard proof, which pins each
// marker arm inside the shipped candidate predicate (issue #7794 seam).
const DeferredRelationshipFamilyFluxGitRepositoryMarkerSQL = `(
            jsonb_typeof(fact.payload -> 'parsed_file_data' -> 'flux_git_repositories') = 'array'
            AND jsonb_array_length(fact.payload -> 'parsed_file_data' -> 'flux_git_repositories') > 0
          )`

// deferredRelationshipFamilyArtifactTypeListSQL is the extractor-family
// artifact_type set shared by the fact-payload candidate predicate and the
// sidecar predicate (issue #10875). Both predicates embed this exact
// fragment; TestDeferredRelationshipFamilySidecarSharesCandidateLiterals
// pins the sharing so the two cannot drift.
const deferredRelationshipFamilyArtifactTypeListSQL = `(
            'terraform',
            'terraform_hcl',
            'terraform_template_text',
            'terragrunt',
            'helm',
            'argocd',
            'dockerfile',
            'docker_compose',
            'github_actions_workflow'
          )`

// deferredRelationshipFamilyPathPatternSQL is the extractor-family path
// alternation shared by the fact-payload candidate predicate and the sidecar
// predicate (issue #10875). The candidate predicate embeds the identical
// literal inline; TestDeferredRelationshipFamilySidecarSharesCandidateLiterals
// asserts it contains this exact fragment so the two cannot drift.
const deferredRelationshipFamilyPathPatternSQL = `'(^|/)(dockerfile(\.[^/]*)?|jenkinsfile(\.[^/]*)?|puppetfile|berksfile)$|(^|/)docker-compose\.ya?ml$|(^|/)compose\.ya?ml$|(^|/)\.github/workflows/[^/]+\.ya?ml$|(^|/)applicationsets?/.*\.ya?ml$|(^|/)argocd/.*\.ya?ml$|(^|/)values([^/]*)\.ya?ml$|(^|/)chart\.ya?ml$|(^|/)kustomization\.ya?ml$|(^|/)(playbooks|roles|group_vars|host_vars|inventories)/|(^|/)inventory($|/)|\.(tf|tf\.json|tfvars|tfvars\.json|hcl|tpl)$'`

// deferredRelationshipFamilyYamlSuffixPatternSQL is the yaml-suffix gate the
// ArgoCD and Salt content markers share with the sidecar predicate
// (issue #10875). Same sharing contract as the path pattern.
const deferredRelationshipFamilyYamlSuffixPatternSQL = `'\.ya?ml$'`

// DeferredRelationshipFamilySidecarPredicateSQL recomposes the candidate
// predicate from the version-1 sidecar columns instead of the fact payload
// (issue #10875). Each arm is the column form of the corresponding
// fact-payload arm: the artifact and path literals are the shared fragments
// above, the content conjunctions are the precomputed marker bits the Go
// writer derives with the same semantics (including the Salt LIKE-plus-
// regex conjunction and the 'kind: application' substring covering
// applicationsets), and the yaml-suffix gate is the shared pattern. The
// reader trusts it per row (keys_version >= 1); the writer derives every bit
// from the fact's own envelope alone (#10861 D2).
const DeferredRelationshipFamilySidecarPredicateSQL = `(
          ref.fact_kind = 'gcp_cloud_relationship'
          OR ref.artifact_type_norm IN ` + deferredRelationshipFamilyArtifactTypeListSQL + `
          OR ref.artifact_type_norm LIKE 'ansible_%'
          OR ref.path_norm ~ ` + deferredRelationshipFamilyPathPatternSQL + `
          OR ((ref.artifact_type_norm = 'argocd' OR ref.path_norm ~ ` + deferredRelationshipFamilyYamlSuffixPatternSQL + `) AND ref.content_argocd_kind)
          OR ((ref.path_norm ~ ` + deferredRelationshipFamilyYamlSuffixPatternSQL + `) AND ref.content_salt_gitfs)
          OR ref.flux_git_nonempty
        )`

// DeferredRelationshipFamilySidecarMarkerArmSQL selects version-1 sidecar
// rows holding any unconditional payload marker (issue #10875). The $1 LIKE
// arm carries these markers for the legacy path; the keyed path reads the
// precomputed um_* bits instead of scanning payload text.
const DeferredRelationshipFamilySidecarMarkerArmSQL = `(ref.um_kind_application
            OR ref.um_artifact_argocd
            OR ref.um_argocd_applications
            OR ref.um_argocd_applicationsets
            OR ref.um_flux_git_repositories)`

const DeferredRelationshipFamilyCandidatePredicateSQL = `(
          fact.fact_kind = 'gcp_cloud_relationship'
          OR ` + deferredRelationshipFamilyArtifactTypeSQL + ` IN ` + deferredRelationshipFamilyArtifactTypeListSQL + `
          OR ` + deferredRelationshipFamilyArtifactTypeSQL + ` LIKE 'ansible_%'
          OR ` + DeferredRelationshipFamilyPathSQL + ` ~ '(^|/)(dockerfile(\.[^/]*)?|jenkinsfile(\.[^/]*)?|puppetfile|berksfile)$|(^|/)docker-compose\.ya?ml$|(^|/)compose\.ya?ml$|(^|/)\.github/workflows/[^/]+\.ya?ml$|(^|/)applicationsets?/.*\.ya?ml$|(^|/)argocd/.*\.ya?ml$|(^|/)values([^/]*)\.ya?ml$|(^|/)chart\.ya?ml$|(^|/)kustomization\.ya?ml$|(^|/)(playbooks|roles|group_vars|host_vars|inventories)/|(^|/)inventory($|/)|\.(tf|tf\.json|tfvars|tfvars\.json|hcl|tpl)$'
          OR ` + deferredRelationshipFamilyArgoCDContentMarkerSQL + `
          OR ` + DeferredRelationshipFamilySaltGitfsContentMarkerSQL + `
          OR ` + DeferredRelationshipFamilyFluxGitRepositoryMarkerSQL + `
        )`

// DeferredRelationshipFamilyPayloadFactsFilterSQL is the WHERE arm the
// deferred scoped-fact query builds from the relationship-family candidate
// predicate. Exported for the root guard proof that the shipped query embeds
// the relationship-family arm (issue #7794 extraction seam).
const DeferredRelationshipFamilyPayloadFactsFilterSQL = `WHERE ` + DeferredRelationshipFamilyCandidatePredicateSQL
