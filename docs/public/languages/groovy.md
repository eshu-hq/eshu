# Groovy Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `groovy`
- Family: `language`
- Parser: `DefaultEngine (groovy)`
- Entrypoint: `go/internal/parser/groovy_language.go`
- Unit test suite: `go/internal/parser/groovy_language_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Jenkins shared libraries | `jenkins-shared-libraries` | supported | `shared_libraries` | `shared_libraries` | `property:File.shared_libraries` | `go/internal/parser/groovy_language_test.go::TestDefaultEngineParsePathGroovyJenkinsfile` | Compose-backed fixture verification | - |
| Jenkins pipeline entry calls | `jenkins-pipeline-calls` | supported | `pipeline_calls` | `pipeline_calls` | `property:File.pipeline_calls` | `go/internal/parser/groovy_language_test.go::TestDefaultEngineParsePathGroovyJenkinsfile`, `go/internal/parser/groovy/pipeline_metadata_gate_test.go::TestParsePipelineMetadataGatedToJenkinsArtifacts` | Compose-backed fixture verification | Jenkins pipeline metadata (this row and the other `shared_libraries`/`entry_points`/`jenkins_pipeline_metadata`/`shell_commands`/`ansible_playbook_hints` rows) is populated only for Jenkins artifacts: files named `Jenkinsfile`/`Jenkinsfile.*` or shared-library scripts under `vars/*.groovy`. An ordinary `.groovy` class with a method named `pipelineDeploy` no longer fabricates Jenkins evidence. |
| Jenkins deployment entry points | `jenkins-entry-points` | supported | `entry_points` | `entry_points` | `property:File.entry_points` | `go/internal/parser/groovy_language_test.go::TestDefaultEngineParsePathGroovyJenkinsfile` | Compose-backed fixture verification | - |
| Jenkins deployment hints | `jenkins-deploy-hints` | supported | `jenkins_pipeline_metadata` | `use_configd, has_pre_deploy` | `property:File` | `go/internal/parser/groovy_language_test.go::TestDefaultEngineParsePathGroovyJenkinsfile` | Compose-backed fixture verification | - |
| Jenkins shell command hints | `jenkins-shell-commands` | supported | `shell_commands` | `shell_commands` | `property:File.shell_commands` | `go/internal/parser/groovy_language_test.go::TestDefaultEngineParsePathGroovyJenkinsfileAnsibleHints` | Compose-backed fixture verification | - |
| Jenkins Ansible playbook hints | `jenkins-ansible-hints` | supported | `ansible_playbook_hints` | `playbook, command` | `property:File.ansible_playbook_hints` | `go/internal/parser/groovy_language_test.go::TestDefaultEngineParsePathGroovyJenkinsfileAnsibleHints` | Compose-backed fixture verification | - |

## Parity Notes
- Jenkins pipeline metadata (`shared_libraries`, `pipeline_calls`,
  `entry_points`, `jenkins_pipeline_metadata`, `shell_commands`,
  `ansible_playbook_hints`) is extracted only for Jenkins artifacts — files
  named `Jenkinsfile`/`Jenkinsfile.*` or shared-library scripts under
  `vars/*.groovy` — not for arbitrary `.groovy` files. An ordinary class with a
  method named `pipelineDeploy` no longer fabricates Jenkins evidence
  (`go/internal/parser/groovy/pipeline_metadata_gate_test.go::TestParsePipelineMetadataGatedToJenkinsArtifacts`).
- Jenkinsfile parsing now preserves explicit shared-library refs, including
  `library(...)` step forms, and explicit GitHub repository URLs for the
  relationship layer.
- Repository-context query surfaces now also summarize controller artifacts for
  `Jenkinsfile` inputs, so the Groovy parser output feeds both the relationship
  map and the read-side controller narrative.

## Framework And Library Support

Supported today:

- Jenkinsfile declarative or scripted pipeline entrypoints are modeled as
  framework roots.
- Jenkins shared-library calls, deployment entrypoints, shell command hints,
  and Ansible playbook hints are captured as controller evidence.

Not claimed today:

- Generic Groovy framework semantics, custom shared-library DSL behavior,
  dynamic dispatch, closure delegates, and Jenkins runtime loading remain
  outside the exactness boundary.

## Parser Performance
- `PipelineMetadata` (`go/internal/parser/groovy/metadata.go`) gates the
  Ansible-hint extraction loop behind a cheap `strings.Contains(command,
  "ansible-playbook")` precondition before invoking the `groovyAnsiblePattern`
  regex on each already-extracted shell command. The precondition is a
  provable superset of the regex: the pattern requires the literal
  `ansible-playbook` substring to match at all, so any command the
  precondition skips could never have matched, and parser output stays
  byte-identical.
- The gate matters for shared-library Jenkins "vars" files with many discrete
  short `sh` steps (one call per pipeline step), where
  `regexp.FindStringSubmatch`'s fixed per-call overhead dominates; it is a
  no-op for a single large embedded shell script, where the stdlib regex
  engine's own required-literal prefix search already does the equivalent
  work internally.
- Byte-identical output was verified with a one-time `0/0` differential (the
  opt-in `GROOVY_PARSE_DUMP` harness in
  `go/internal/parser/groovy/equivalence_dump_test.go` — MANUAL, not a CI
  gate) against the real fixture corpus and a synthetic worst-case Jenkinsfile
  with hundreds of non-Ansible shell steps plus one Ansible step. Standing
  protection for this behavior is the `internal/parser/groovy` package test
  suite (including a characterization test for an Ansible command found
  among many non-Ansible commands) plus the B-12 golden snapshot. See epic
  #4831 and issue #4845.

## Known Limitations
- Generic Groovy source is indexed conservatively; the current parser focuses on Jenkins pipeline metadata rather than broad class and method extraction
- Jenkins metadata is strongest for Jenkinsfile-style entrypoints and may not detect custom shared-library DSLs that hide deployment semantics behind opaque helper calls
- Jenkins controller hints are intentionally shallow; deeper controller-driven automation meaning is assembled later from Ansible, inventory, vars, runtime-family enrichment, and repository-context controller-artifact summaries
