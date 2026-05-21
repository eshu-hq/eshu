# Groovy Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `groovy`
- Family: `language`
- Parser: `DefaultEngine (groovy)`
- Entrypoint: `go/internal/parser/groovy_language.go`
- Fixture repo: `tests/fixtures/ecosystems/groovy_comprehensive/`
- Unit test suite: `go/internal/parser/groovy_language_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Jenkins shared libraries | `jenkins-shared-libraries` | supported | `shared_libraries` | `shared_libraries` | `property:File.shared_libraries` | `go/internal/parser/groovy_language_test.go::TestDefaultEngineParsePathGroovyJenkinsfile` | Compose-backed fixture verification | - |
| Jenkins pipeline entry calls | `jenkins-pipeline-calls` | supported | `pipeline_calls` | `pipeline_calls` | `property:File.pipeline_calls` | `go/internal/parser/groovy_language_test.go::TestDefaultEngineParsePathGroovyJenkinsfile` | Compose-backed fixture verification | - |
| Jenkins deployment entry points | `jenkins-entry-points` | supported | `entry_points` | `entry_points` | `property:File.entry_points` | `go/internal/parser/groovy_language_test.go::TestDefaultEngineParsePathGroovyJenkinsfile` | Compose-backed fixture verification | - |
| Jenkins deployment hints | `jenkins-deploy-hints` | supported | `jenkins_pipeline_metadata` | `use_configd, has_pre_deploy` | `property:File` | `go/internal/parser/groovy_language_test.go::TestDefaultEngineParsePathGroovyJenkinsfile` | Compose-backed fixture verification | - |
| Jenkins shell command hints | `jenkins-shell-commands` | supported | `shell_commands` | `shell_commands` | `property:File.shell_commands` | `go/internal/parser/groovy_language_test.go::TestDefaultEngineParsePathGroovyJenkinsfileAnsibleHints` | Compose-backed fixture verification | - |
| Jenkins Ansible playbook hints | `jenkins-ansible-hints` | supported | `ansible_playbook_hints` | `playbook, command` | `property:File.ansible_playbook_hints` | `go/internal/parser/groovy_language_test.go::TestDefaultEngineParsePathGroovyJenkinsfileAnsibleHints` | Compose-backed fixture verification | - |

## Parity Notes
- Jenkinsfile parsing now preserves explicit shared-library refs, including
  `library(...)` step forms, and explicit GitHub repository URLs for the
  relationship layer.
- Repository-context query surfaces now also summarize controller artifacts for
  `Jenkinsfile` inputs, so the Groovy parser output feeds both the relationship
  map and the read-side controller narrative.

## Known Limitations
- Generic Groovy source is indexed conservatively; the current parser focuses on Jenkins pipeline metadata rather than broad class and method extraction
- Jenkins metadata is strongest for Jenkinsfile-style entrypoints and may not detect custom shared-library DSLs that hide deployment semantics behind opaque helper calls
- Jenkins controller hints are intentionally shallow; deeper controller-driven automation meaning is assembled later from Ansible, inventory, vars, runtime-family enrichment, and repository-context controller-artifact summaries
