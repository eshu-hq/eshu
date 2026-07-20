#!/usr/bin/env bash
#
# maturity-drift-guard-language-extensions.sh - the extension -> language
# registry scripts/verify-maturity-drift-guard.sh uses to attribute a
# non-"_comprehensive" corpus fixture (an app-shaped "real repo" fixture such
# as lib-common, orders-api, or api-svc, per the Real-Repo Validation grade
# definition in docs/public/languages/support-maturity.md) to a
# specs/language-feature-parity-ledger.v1.yaml language key.
#
# A "_comprehensive"-suffixed fixture (go_comprehensive, python_comprehensive,
# terraform_comprehensive, ...) already names its language directly (the
# suffix is stripped and the remainder IS the ledger key); this registry only
# matters for the remaining fixtures, which are identified by the source-code
# extensions they actually contain.
#
# Deliberately scoped to "programming language" extensions only. Config/IaC
# extensions (yaml, yml, hcl, json, ...) are intentionally excluded:
#   - Terraform/Terragrunt are already covered by the "_comprehensive" suffix
#     rule (terraform_comprehensive, terragrunt_comprehensive are staged
#     directly).
#   - Every YAML/JSON-backed row in support-maturity.md (ArgoCD,
#     CloudFormation, Crossplane, Helm, JSON Config, Kubernetes, Kustomize)
#     is graded "-" (Query Surfacing opted out) and is filtered out by the
#     verifier before this registry is ever consulted for it, so mapping
#     yaml/json here would never change a result and would only invite a
#     false attribution the next time a yaml-shaped fixture is staged for an
#     unrelated reason.
#
# Format: one "extension:ledger_key" entry per line. bash 3.2 (macOS stock)
# compatible: a plain indexed array, never `declare -A` (repo rule).
MATURITY_DRIFT_GUARD_EXT_LANG=(
	"go:go"
	"py:python"
	"rb:ruby"
	"java:java"
	"js:javascript"
	"jsx:javascript"
	"ts:typescript"
	"tsx:typescriptjsx"
	"rs:rust"
	"swift:swift"
	"kt:kotlin"
	"scala:scala"
	"php:php"
	"pl:perl"
	"cs:csharp"
	"cpp:cpp"
	"cc:cpp"
	"cxx:cpp"
	"hpp:cpp"
	"c:c"
	"h:c"
	"hs:haskell"
	"ex:elixir"
	"exs:elixir"
	"dart:dart"
	"groovy:groovy"
	"sql:sql"
)
