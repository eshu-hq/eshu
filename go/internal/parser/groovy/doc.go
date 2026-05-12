// Package groovy extracts Jenkins and Groovy parser evidence that can stay
// independent from the parent parser dispatch package.
//
// Parse and PreScan own the Groovy adapter implementation while the parent
// parser keeps registry dispatch and compatibility wrappers. Parse emits
// lexical class, function, and function-call entities, and marks Jenkinsfile
// entrypoints, including files that also declare helper functions, plus absolute
// or repository-relative vars/*.groovy call methods with dead-code root
// metadata. PipelineMetadata returns typed delivery
// evidence for shared libraries, pipeline calls, shell commands, Ansible
// playbooks, entry points, and configd/pre-deploy flags. Metadata.Map preserves
// the parent parser payload shape used by query and relationship callers.
package groovy
