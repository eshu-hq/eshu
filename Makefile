# Eshu convenience targets. The canonical gates live in scripts/ (and CI under
# .github/workflows/); this Makefile only provides ergonomic entry points.
.PHONY: help pre-pr

help: ## List available targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

pre-pr: ## Run the local CI-mirror gate (lint/build/vet/test/docs) before opening a PR
	@bash scripts/dev/pre-pr.sh
