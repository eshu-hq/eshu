// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package groovy extracts Jenkins and Groovy parser evidence that can stay
// independent from the parent parser dispatch package.
//
// Parse, ParseWithParser, PreScan, and PreScanWithParser own the Groovy adapter
// implementation while the parent parser keeps registry dispatch and
// compatibility wrappers. Parse emits tree-sitter-backed class, method, import,
// and method-call entities, and marks Jenkinsfile entrypoints, including files
// that also declare helper functions, plus absolute or repository-relative
// vars/*.groovy call methods with dead-code root metadata. PipelineMetadata
// remains lexical and returns typed delivery
// evidence for shared libraries, pipeline calls, shell commands, Ansible
// playbooks, entry points, and configd/pre-deploy flags. Metadata.Map preserves
// the parent parser payload shape used by query and relationship callers.
//
// The engine-level payload contract is pinned by groovy_language_test.go and
// groovy_jenkins_golden_fixture_test.go in this directory, which compile as
// the external package groovy_test and drive parser.DefaultEngine().ParsePath
// the way a caller would. Only that external test package imports the parent
// parser; production code and the in-package tests never do.
package groovy
