# Eshu convenience targets. The canonical gates live in scripts/ (and CI under
# .github/workflows/); this Makefile only provides ergonomic entry points.
.PHONY: help pre-pr pre-pr-full

help: ## List available targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

pre-pr: ## Run the local CI-mirror gate (lint/build/vet/test/exactness/race) before opening a PR
	@bash scripts/dev/pre-pr.sh

pre-pr-full: ## Like pre-pr but with whole-module race (go test ./... -race) for high-risk PRs
	@ESHU_PRE_PR_FULL_RACE=1 bash scripts/dev/pre-pr.sh
