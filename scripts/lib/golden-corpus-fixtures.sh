#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Minimal golden corpus inventory. The comprehensive fixtures exercise the
# per-language parsers. lib-common (publisher of @acme/lib-common) + orders-api
# (consumer of it) form a cross-repo DEPENDS_ON (rc-3): the package-registry
# cassette carries a source_hint mapping @acme/lib-common to
# github.com/acme/lib-common, and ESHU_GITHUB_ORG=acme makes both fixtures'
# synthesized remotes match that org.
#
# cloudformation_comprehensive (#5954) existed on disk for the YAML parser's
# unit tests (go/internal/parser/engine_yaml_cloudformation_lines_test.go) but
# was never staged here, so the live gate had zero real coverage for any
# CloudFormation node label -- Resource, Parameter, Output, Condition, Import,
# and Export alike. Staging it gives CloudFormationCondition/Import/Export
# their first real end-to-end proof.

corpus_fixtures=(
	go_comprehensive
	php_comprehensive
	python_comprehensive
	sql_comprehensive
	terraform_comprehensive
	terraform_local_backend_demo
	terragrunt_comprehensive
	cloudformation_comprehensive
	kubernetes_comprehensive
	helm_argocd_platform
	lib-common
	orders-api
	deployable-source
	deployable-config
	kustomize-deployable-overlay
	ansible-platform-playbooks
	ansible-shared-roles
	jenkins-ci-pipelines
	puppet-platform-modules
	chef-cookbooks
	salt-formulas
	helm-umbrella-chart
	helm-template-chart
	api-svc
	container-base-lineage
	container-ci-lineage
	ruby_rails_app
	dart_comprehensive
	swift_vapor_app
	github_actions_workflows
	supply-chain-demo-db
)
