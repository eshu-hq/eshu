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

corpus_fixtures=(
	go_comprehensive
	python_comprehensive
	sql_comprehensive
	terraform_comprehensive
	terragrunt_comprehensive
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
	ruby_rails_app
	dart_comprehensive
	swift_vapor_app
	github_actions_workflows
)
