# Eshu convenience targets. The canonical gates live in scripts/ (and CI under
# .github/workflows/); this Makefile only provides ergonomic entry points.
# #9066: go-sitter-forest/perl does not compile as C23, which GCC 15 on glibc 2.43
# defaults to. Same rule as scripts/lib/cgo-cflags.sh, which has the details: start
# from the caller's CGO_CFLAGS, then `go env CGO_CFLAGS`, then Go's -O2 -g default
# (setting the variable replaces it), and respect a caller's own -std=.
eshu_cgo_base := $(or $(CGO_CFLAGS),$(shell go env CGO_CFLAGS 2>/dev/null),-O2 -g)
ifeq (,$(findstring -std=,$(eshu_cgo_base)))
export CGO_CFLAGS := $(eshu_cgo_base) -std=gnu17
else
export CGO_CFLAGS := $(eshu_cgo_base)
endif

.PHONY: help pre-push pre-pr pre-pr-full frontend-preflight security-preflight prove

help: ## List available targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

pre-push: ## Run the fast local floor (changed-package lint/build/vet/test/exactness) before every push
	@bash scripts/dev/pre-push.sh

pre-pr: ## Run the local CI-mirror gate (lint/build/vet/test/exactness/race) before opening a PR; recommended for risky changes
	@bash scripts/dev/pre-pr.sh

pre-pr-full: ## Like pre-pr but with whole-module race and advisory gates for high-risk PRs
	@ESHU_PRE_PR_FULL_RACE=1 ESHU_PRE_PR_INCLUDE_ADVISORY=1 bash scripts/dev/pre-pr.sh

frontend-preflight: ## Run the selected frontend gates (typecheck/test/build/a11y/eslint/audit) for changed paths
	@bash scripts/dev/frontend-preflight.sh

security-preflight: ## Run the selected security gates (gosec/govulncheck/nancy/trivy-fs) for changed paths
	@bash scripts/dev/security-preflight.sh

prove: ## Run the credential-free Ifá conformance mirror (contract test/hermetic mirrors/coverage, plus the path-selected Docker matrix)
	@bash scripts/dev/prove.sh
