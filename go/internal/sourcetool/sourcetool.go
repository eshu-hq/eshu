// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sourcetool

// Canonical is the ordered, closed set of source_tool tokens. The order is
// stable and matches docs/public/reference/edge-source-tool-provenance.md.
// Consumers that need enum validation must use IsValid rather than ranging
// over Canonical directly, to keep the implementation change-resilient.
var Canonical = []string{
	"terraform",
	"terragrunt",
	"helm",
	"kustomize",
	"argocd",
	"flux",
	"ansible",
	"puppet",
	"chef",
	"salt",
	"jenkins",
	"github_actions",
	"docker",
	"docker_compose",
	"gcp",
	"atlantis",
	"gitlab",
	"gomod",
	"npm",
	"pip",
	"maven",
	"cargo",
	"aws",
	"azure",
	"kubernetes",
	"oci",
	"unknown",
}

// validTokens is the lookup set built from Canonical at init time. It is
// package-private; callers use IsValid.
var validTokens = func() map[string]struct{} {
	m := make(map[string]struct{}, len(Canonical))
	for _, t := range Canonical {
		m[t] = struct{}{}
	}
	return m
}()

// IsValid reports whether token is a member of the canonical vocabulary.
// The caller is responsible for lowercasing and trimming the token before
// calling IsValid; this function performs an exact match.
func IsValid(token string) bool {
	_, ok := validTokens[token]
	return ok
}
