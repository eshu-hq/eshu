// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctruth

import (
	"regexp"
	"sort"
	"strings"
)

var (
	containerImageDirectivePattern = regexp.MustCompile(`(?i)\bimage:\s*["']?([^"'\s#]+)["']?`)
	containerImageFromPattern      = regexp.MustCompile(`(?i)^\s*FROM\s+([^"'\s#]+)`)
	containerImageTokenPattern     = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._:-]*(?:/[A-Za-z0-9][A-Za-z0-9._-]*)*(?::[A-Za-z0-9._-]+|@sha256:[a-fA-F0-9]{64})`)
	containerImageEnvDefault       = regexp.MustCompile(`\$\{[^:}]+:-([^}]+)\}`)
	containerImageRepository       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
	containerImageTag              = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
	containerImageDigest           = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
)

// ContainerImageResolver checks one normalized container image claim against caller-owned truth.
type ContainerImageResolver func(DocumentInput, string) ContainerImageResolution

// ContainerImageResolution is the caller-supplied truth result for a container image claim.
type ContainerImageResolution struct {
	Supported bool
	Exists    bool
}

func containerImageClaimsFromLine(line string, lineNumber int) []extractedClaim {
	claims := []extractedClaim{}
	for _, imageRef := range containerImageRefsFromImageDirectives(line) {
		claims = append(claims, extractedClaim{
			claimType:  ClaimTypeContainerImageRef,
			text:       imageRef,
			normalized: imageRef,
			line:       lineNumber,
		})
	}
	return claims
}

// ContainerImageRefsFromText extracts explicit tagged or digested image refs from bounded text.
func ContainerImageRefsFromText(content string) []string {
	refs := map[string]struct{}{}
	for _, line := range strings.Split(content, "\n") {
		for _, ref := range containerImageRefsFromImageDirectives(line) {
			refs[ref] = struct{}{}
		}
		for _, ref := range containerImageRefsFromFromDirective(line) {
			refs[ref] = struct{}{}
		}
	}
	if len(refs) == 0 {
		for _, ref := range containerImageRefsFromToken(content) {
			refs[ref] = struct{}{}
		}
	}
	out := make([]string, 0, len(refs))
	for ref := range refs {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func containerImageRefsFromImageDirectives(line string) []string {
	refs := []string{}
	for _, match := range containerImageDirectivePattern.FindAllStringSubmatch(line, -1) {
		refs = append(refs, containerImageRefsFromToken(match[1])...)
	}
	return refs
}

func containerImageRefsFromFromDirective(line string) []string {
	match := containerImageFromPattern.FindStringSubmatch(line)
	if len(match) != 2 {
		return nil
	}
	return containerImageRefsFromToken(match[1])
}

func containerImageRefsFromToken(raw string) []string {
	text := strings.TrimSpace(raw)
	if strings.Contains(text, "://") {
		return nil
	}
	defaultMatches := containerImageEnvDefault.FindAllStringSubmatch(text, -1)
	if len(defaultMatches) > 0 {
		defaults := make([]string, 0, len(defaultMatches))
		for _, match := range defaultMatches {
			defaults = append(defaults, match[1])
		}
		text = strings.Join(defaults, "\n")
	}
	refs := []string{}
	for _, match := range containerImageTokenPattern.FindAllString(text, -1) {
		if ref := NormalizeContainerImageRefClaim(match); ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

// NormalizeContainerImageRefClaim returns a canonical explicit image ref or an empty string.
func NormalizeContainerImageRefClaim(raw string) string {
	text := strings.Trim(strings.TrimSpace(raw), `"'`)
	text = strings.TrimRight(text, ".,);")
	if text == "" || strings.ContainsAny(text, " \t\n\r") {
		return ""
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return ""
	}
	if strings.ContainsAny(text, "${}") {
		return ""
	}
	if strings.Contains(text, "@sha256:") {
		parts := strings.Split(text, "@sha256:")
		if len(parts) != 2 || !containerImageDigest.MatchString(parts[1]) || !looksLikeImageRepository(parts[0]) {
			return ""
		}
		return parts[0] + "@sha256:" + strings.ToLower(parts[1])
	}
	tagIndex := strings.LastIndex(text, ":")
	slashIndex := strings.LastIndex(text, "/")
	if tagIndex <= slashIndex || tagIndex == len(text)-1 {
		return ""
	}
	repository := text[:tagIndex]
	tag := text[tagIndex+1:]
	if !containerImageTag.MatchString(tag) {
		return ""
	}
	if !strings.Contains(repository, "/") && looksLikeHostPortWithoutPath(repository) {
		return ""
	}
	if !looksLikeImageRepository(repository) {
		return ""
	}
	return text
}

func normalizeInlineContainerImageRefClaim(raw string) string {
	ref := NormalizeContainerImageRefClaim(raw)
	if ref == "" {
		return ""
	}
	repository := ref
	if digestIndex := strings.Index(repository, "@sha256:"); digestIndex >= 0 {
		repository = repository[:digestIndex]
	} else if tagIndex := strings.LastIndex(repository, ":"); tagIndex >= 0 {
		repository = repository[:tagIndex]
	}
	if !strings.Contains(repository, "/") {
		return ""
	}
	if looksLikeFileLineReference(repository) {
		return ""
	}
	return ref
}

func looksLikeHostPortWithoutPath(repository string) bool {
	return strings.EqualFold(repository, "localhost") || strings.Contains(repository, ".")
}

func looksLikeFileLineReference(repository string) bool {
	parts := strings.Split(repository, "/")
	last := parts[len(parts)-1]
	if !strings.Contains(last, ".") {
		return false
	}
	first := parts[0]
	return !strings.Contains(first, ".") && !strings.Contains(first, ":") && !strings.EqualFold(first, "localhost")
}

func looksLikeImageRepository(repository string) bool {
	if repository == "" || strings.HasPrefix(repository, ".") || strings.HasSuffix(repository, ".") {
		return false
	}
	if !containerImageRepository.MatchString(repository) {
		return false
	}
	for i, part := range strings.Split(repository, "/") {
		colon := strings.Count(part, ":")
		if colon == 0 {
			continue
		}
		if i != 0 || colon > 1 {
			return false
		}
		port := part[strings.LastIndex(part, ":")+1:]
		if port == "" || strings.Trim(port, "0123456789") != "" {
			return false
		}
	}
	return true
}
