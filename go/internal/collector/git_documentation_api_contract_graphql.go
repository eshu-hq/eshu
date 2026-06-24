// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

var graphqlFieldPattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(?:\(|:)`)

func extractGraphQLSDLDocumentation(
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
) (facts.DocumentationDocumentPayload, []facts.DocumentationSectionPayload, []facts.DocumentationLinkPayload) {
	bodyText, warnings := boundedDocumentationBodyBytes(body, apiContractMaxBodyBytes)
	drafts := graphqlSectionDrafts(bodyText)
	if len(drafts) > apiContractMaxSections {
		drafts = drafts[:apiContractMaxSections]
		warnings = append(warnings, "section_limit_exceeded")
	}
	documentID := gitDocumentationDocumentID(repo.ID, relativePath)
	revisionID := firstNonEmptyString(commitSHA, digest, "unknown")
	sections := documentationSectionsFromDrafts(documentID, revisionID, relativePath, "graphql_sdl", drafts)
	document := apiContractDocumentPayload(repo, relativePath, digest, commitSHA, bodyText, "graphql_sdl", warnings, sections)
	return document, sections, nil
}

func graphqlSectionDrafts(bodyText string) []markdownSectionDraft {
	lines := strings.Split(bodyText, "\n")
	drafts := []markdownSectionDraft{}
	currentType := ""
	pendingDescription := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if description, ok := graphqlDescriptionLine(trimmed); ok {
			pendingDescription = description
			continue
		}
		if strings.HasPrefix(trimmed, "type ") {
			fields := strings.Fields(strings.TrimPrefix(trimmed, "type "))
			if len(fields) == 0 {
				currentType = ""
				pendingDescription = ""
				continue
			}
			currentType = fields[0]
			pendingDescription = ""
			continue
		}
		if trimmed == "}" {
			currentType = ""
			pendingDescription = ""
			continue
		}
		if currentType == "" {
			continue
		}
		match := graphqlFieldPattern.FindStringSubmatch(trimmed)
		if len(match) != 2 {
			continue
		}
		fieldName := match[1]
		heading := currentType + "." + fieldName
		content := []string{}
		if pendingDescription != "" {
			content = append(content, pendingDescription)
		}
		content = append(content, "Signature: "+trimmed)
		drafts = append(drafts, apiContractDraft(
			1,
			heading,
			"field-"+markdownAnchor(heading),
			fmt.Sprintf("line:%d", i+1),
			map[string]string{"contract_kind": "graphql_sdl", "type_name": currentType, "field_name": fieldName},
			content,
		))
		pendingDescription = ""
	}
	return drafts
}

func graphqlDescriptionLine(line string) (string, bool) {
	if strings.HasPrefix(line, `"""`) && strings.HasSuffix(line, `"""`) && len(line) >= 6 {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, `"""`), `"""`)), true
	}
	if strings.HasPrefix(line, `"`) && strings.HasSuffix(line, `"`) && len(line) >= 2 {
		return strings.TrimSpace(strings.Trim(line, `"`)), true
	}
	return "", false
}
