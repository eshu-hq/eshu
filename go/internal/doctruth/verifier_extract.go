package doctruth

import (
	"sort"
	"strings"
)

func extractClaims(content string) []extractedClaim {
	claims := []extractedClaim{}
	for lineNumber, line := range strings.Split(content, "\n") {
		lineClaims := map[string]extractedClaim{}
		for _, match := range backtickPattern.FindAllStringSubmatch(line, -1) {
			addExtractedClaim(lineClaims, classifyClaim(match[1], lineNumber+1))
		}
		for _, claim := range localPathClaimsFromMarkdownLinks(line, lineNumber+1) {
			addExtractedClaim(lineClaims, claim)
		}
		for _, claim := range containerImageClaimsFromLine(line, lineNumber+1) {
			addExtractedClaim(lineClaims, claim)
		}
		for _, match := range httpEndpointPattern.FindAllStringSubmatch(line, -1) {
			addExtractedClaim(lineClaims, extractedClaim{
				claimType:  ClaimTypeHTTPEndpoint,
				text:       match[0],
				normalized: endpointKey(match[1], match[2]),
				line:       lineNumber + 1,
			})
		}
		for _, match := range envVarPattern.FindAllString(line, -1) {
			addExtractedClaim(lineClaims, extractedClaim{
				claimType:  ClaimTypeEnvironmentVariable,
				text:       match,
				normalized: match,
				line:       lineNumber + 1,
			})
		}
		keys := make([]string, 0, len(lineClaims))
		for key := range lineClaims {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			claims = append(claims, lineClaims[key])
		}
	}
	return claims
}

func classifyClaim(raw string, line int) extractedClaim {
	text := normalizeSnippet(raw)
	if command := normalizeEshuCommand(text); command != "" {
		return extractedClaim{claimType: ClaimTypeCLICommand, text: text, normalized: command, line: line}
	}
	if endpoint := httpEndpointPattern.FindStringSubmatch(text); len(endpoint) == 3 {
		return extractedClaim{
			claimType:  ClaimTypeHTTPEndpoint,
			text:       text,
			normalized: endpointKey(endpoint[1], endpoint[2]),
			line:       line,
		}
	}
	if envVar := envVarPattern.FindString(text); envVar != "" && envVar == text {
		return extractedClaim{claimType: ClaimTypeEnvironmentVariable, text: text, normalized: envVar, line: line}
	}
	if localPath := normalizeLocalPathClaim(text); localPath != "" {
		return extractedClaim{claimType: ClaimTypeLocalPath, text: text, normalized: localPath, line: line}
	}
	if imageRef := normalizeInlineContainerImageRefClaim(text); imageRef != "" {
		return extractedClaim{claimType: ClaimTypeContainerImageRef, text: text, normalized: imageRef, line: line}
	}
	lower := strings.ToLower(text)
	for _, prefix := range []string{"terraform ", "kubectl ", "helm ", "aws "} {
		if strings.HasPrefix(lower, prefix) {
			return extractedClaim{claimType: ClaimTypeShellCommand, text: text, normalized: strings.Fields(text)[0], line: line}
		}
	}
	return extractedClaim{}
}

func addExtractedClaim(claims map[string]extractedClaim, claim extractedClaim) {
	if claim.claimType == "" || claim.normalized == "" {
		return
	}
	claims[claim.claimType+"\x00"+claim.normalized] = claim
}
