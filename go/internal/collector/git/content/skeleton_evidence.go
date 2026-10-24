// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"regexp"
	"slices"
	"strings"
)

// The evidence shapes the #8813 record lets join the skeleton allowlist once
// the #8833 differential showed the raw Terraform evidence needs them
// (relationships terraformPatterns). Each shape is exact: a value with
// anything else in it is not kept.
var (
	// evidenceNameValue is an app_repo or app_name value: a name, or owner/name.
	evidenceNameValue = regexp.MustCompile(`^"[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)?"$`)
	// githubURLValue is one quoted github.com URL, in https, http, ssh, git, or
	// git:: form, with at most one userinfo; group 1 is the path before any
	// query or fragment.
	githubURLValue = regexp.MustCompile(`^"(?:git::)?(?:(?:https?|ssh|git)://)?(?:[^"@/\s]*@)?(?i:github\.com)[:/]([^"?#\s]*)(?:[?#][^"\s]*)?"$`)
	// githubOwnerRepo is the owner/repo prefix of a github.com path. The owner
	// follows GitHub's 39-character username cap (letters, digits, hyphens, and
	// the underscore Enterprise Managed Users carry), so a ghp_, gho_, ghs_, or
	// ghu_ token (40 characters) or a github_pat_ token cannot pass as one; only
	// this prefix is kept, never a deeper path.
	githubOwnerRepo = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_-]{0,38}/[A-Za-z0-9._-]{1,100})(?:/|$)`)
	// oidcSubjectValue is a GitHub Actions OIDC subject, repo:owner/name:<claim>.
	oidcSubjectValue = regexp.MustCompile(`^"repo:[A-Za-z0-9._-]+/[A-Za-z0-9._-]+:[A-Za-z0-9._:/*-]*"$`)
	// evidenceURLValue is an app_repo or app_name value that is a plain URL:
	// scheme, host, optional port, and a path of name characters, with no
	// userinfo, query, or fragment.
	evidenceURLValue = regexp.MustCompile(`^"https?://[A-Za-z0-9.-]+(:[0-9]+)?(/[A-Za-z0-9._/-]*)?"$`)
	// githubURLInText finds github.com paths inside free text. The host must
	// follow the start, white space, a quote, "(", "<", "://", or "@", so a
	// lookalike host (notgithub.com), a subdomain, or a github.com string inside
	// another URL's query is not read; group 1 is the path.
	githubURLInText = regexp.MustCompile(`(?:^|[\s"'(<]|://|@)(?i:github\.com)[:/]([A-Za-z0-9._/-]+)`)
)

// skeletonEvidence returns the stored form of a value that holds a Terraform
// evidence shape, and whether it is one. A github.com URL is stored as
// "https://github.com/<owner>/<repo>": the userinfo, deeper path, query, and
// fragment are dropped,
// so a credential carried in them never survives. The caller checks for a
// detector finding first, so a flagged line keeps its finding marker instead.
func skeletonEvidence(key, value string) (string, bool) {
	switch strings.ToLower(strings.Trim(key, `"`)) {
	case "app_repo", "app_name":
		if evidenceNameValue.MatchString(value) || evidenceURLValue.MatchString(value) {
			return value, true
		}
	}
	return skeletonEvidenceValue(value)
}

// skeletonEvidenceValue is skeletonEvidence for a value with no key (a bare
// list element): a github.com URL or an OIDC subject.
func skeletonEvidenceValue(value string) (string, bool) {
	if match := githubURLValue.FindStringSubmatch(value); match != nil {
		if ownerRepo := githubOwnerRepo.FindStringSubmatch(match[1]); ownerRepo != nil {
			return `"https://github.com/` + ownerRepo[1] + `"`, true
		}
	}
	if oidcSubjectValue.MatchString(value) {
		return value, true
	}
	return "", false
}

// githubURLsIn returns every github.com URL in text, normalized to
// "https://github.com/<path>", in order and without duplicates. Only the
// owner/repo path survives: the text around it, a userinfo, a query, and a
// fragment are dropped, so a credential in any of them never reaches the
// skeleton.
func githubURLsIn(text string) []string {
	var urls []string
	for _, match := range githubURLInText.FindAllStringSubmatch(text, -1) {
		ownerRepo := githubOwnerRepo.FindStringSubmatch(match[1])
		if ownerRepo == nil {
			continue
		}
		url := "https://github.com/" + ownerRepo[1]
		if !slices.Contains(urls, url) {
			urls = append(urls, url)
		}
	}
	return urls
}

// splitTrailingComment splits a value from a trailing " #" or " //" comment.
// It reports false when the value has no such comment or the opener sits
// inside a quoted string.
func splitTrailingComment(value string) (string, string, bool) {
	for i := 1; i < len(value); i++ {
		if value[i-1] != ' ' && value[i-1] != '\t' {
			continue
		}
		if value[i] == '#' || strings.HasPrefix(value[i:], "//") {
			head := strings.TrimRight(value[:i], " \t")
			if strings.Count(head, `"`)%2 != 0 {
				return "", "", false
			}
			return head, value[i:], true
		}
	}
	return "", "", false
}
