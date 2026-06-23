package doctruth

import (
	"sort"
	"strconv"
	"strings"
)

type extractedClaim struct {
	claimType  string
	text       string
	normalized string
	line       int
	// byteOffset is the byte position of the first byte of text in the source
	// document. byteLength is len(text). Both are zero when the position cannot
	// be determined (e.g. the text was not found at the expected position after
	// a line split).
	byteOffset int
	byteLength int
}

// MarkdownClaimHints extracts conservative structured claim hints from
// Markdown-family text using the same patterns as the active verifier.
func MarkdownClaimHints(subjectText string, subjectKind string, content string) []ClaimHint {
	subjectText = strings.TrimSpace(subjectText)
	subjectKind = strings.TrimSpace(subjectKind)
	if subjectText == "" || subjectKind == "" {
		return nil
	}
	claims := extractClaims(content)
	hints := make([]ClaimHint, 0, len(claims))
	for _, claim := range claims {
		hints = append(hints, ClaimHint{
			ClaimType:   claim.claimType,
			ClaimText:   claim.text,
			SubjectText: subjectText,
			SubjectKind: subjectKind,
			SourceMetadata: map[string]string{
				"claim_line":       strconv.Itoa(claim.line),
				"claim_source":     "markdown_claim_extractor",
				"normalized_claim": claim.normalized,
			},
		})
	}
	return hints
}

func extractClaims(content string) []extractedClaim {
	claims := []extractedClaim{}
	// lineByteOffset tracks the byte position of the first byte of each line
	// so that per-claim byte windows are document-absolute rather than
	// line-relative. The +1 accounts for the '\n' separator consumed by Split.
	lineByteOffset := 0
	for lineNumber, line := range strings.Split(content, "\n") {
		lineClaims := map[string]extractedClaim{}
		for _, match := range backtickPattern.FindAllStringSubmatchIndex(line, -1) {
			// match[0]:match[1] is the full backtick span; match[2]:match[3] is
			// the captured inner text used by classifyClaim.
			inner := line[match[2]:match[3]]
			claim := classifyClaim(inner, lineNumber+1)
			if claim.claimType != "" {
				// The byte window covers the claim's text field (the normalized
				// snippet without surrounding backticks), located inside the line.
				claimStart := strings.Index(line[match[0]:], claim.text)
				if claimStart >= 0 {
					claim.byteOffset = lineByteOffset + match[0] + claimStart
					claim.byteLength = len(claim.text)
				}
			}
			addExtractedClaim(lineClaims, claim)
		}
		for _, claim := range localPathClaimsFromMarkdownLinks(line, lineNumber+1) {
			stampByteWindow(line, lineByteOffset, &claim)
			addExtractedClaim(lineClaims, claim)
		}
		for _, claim := range containerImageClaimsFromLine(line, lineNumber+1) {
			stampByteWindow(line, lineByteOffset, &claim)
			addExtractedClaim(lineClaims, claim)
		}
		for _, match := range httpEndpointPattern.FindAllStringSubmatchIndex(line, -1) {
			text := line[match[0]:match[1]]
			claim := extractedClaim{
				claimType:  ClaimTypeHTTPEndpoint,
				text:       text,
				normalized: endpointKey(line[match[2]:match[3]], line[match[4]:match[5]]),
				line:       lineNumber + 1,
				byteOffset: lineByteOffset + match[0],
				byteLength: len(text),
			}
			addExtractedClaim(lineClaims, claim)
		}
		for _, loc := range envVarPattern.FindAllStringIndex(line, -1) {
			text := line[loc[0]:loc[1]]
			claim := extractedClaim{
				claimType:  ClaimTypeEnvironmentVariable,
				text:       text,
				normalized: text,
				line:       lineNumber + 1,
				byteOffset: lineByteOffset + loc[0],
				byteLength: len(text),
			}
			addExtractedClaim(lineClaims, claim)
		}
		keys := make([]string, 0, len(lineClaims))
		for key := range lineClaims {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			claims = append(claims, lineClaims[key])
		}
		lineByteOffset += len(line) + 1 // +1 for the '\n' consumed by Split
	}
	return claims
}

// stampByteWindow sets byteOffset and byteLength on a claim whose text was
// extracted without index tracking (local-path and container-image helpers).
// It searches for the first occurrence of claim.text in the line and computes
// the document-absolute offset from lineByteOffset. If the text is not found
// the byte window is left at zero.
func stampByteWindow(line string, lineByteOffset int, claim *extractedClaim) {
	if claim.text == "" {
		return
	}
	idx := strings.Index(line, claim.text)
	if idx < 0 {
		return
	}
	claim.byteOffset = lineByteOffset + idx
	claim.byteLength = len(claim.text)
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
	if strings.HasPrefix(text, "terraform/") {
		terraformAddress := NormalizeTerraformAddressClaim(text)
		return extractedClaim{claimType: ClaimTypeTerraformAddress, text: text, normalized: terraformAddress, line: line}
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
