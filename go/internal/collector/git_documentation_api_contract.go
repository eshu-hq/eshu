// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"gopkg.in/yaml.v3"
)

const apiContractMaxSections = 80

func apiContractFormatForPath(relativePath string) (gitDocumentationFormat, bool) {
	base := strings.ToLower(path.Base(relativePath))
	ext := strings.ToLower(path.Ext(base))
	if ext != ".json" && ext != ".yaml" && ext != ".yml" {
		return gitDocumentationFormat{}, false
	}
	switch {
	case strings.Contains(base, "openapi"), strings.Contains(base, "oas"):
		return gitDocumentationFormat{format: "openapi", language: apiContractLanguage(ext)}, true
	case strings.Contains(base, "swagger"):
		return gitDocumentationFormat{format: "swagger", language: apiContractLanguage(ext)}, true
	case strings.Contains(base, "asyncapi"):
		return gitDocumentationFormat{format: "asyncapi", language: apiContractLanguage(ext)}, true
	default:
		return gitDocumentationFormat{}, false
	}
}

func isPotentialStructuredAPIContractPath(relativePath string) bool {
	switch strings.ToLower(path.Ext(relativePath)) {
	case ".json", ".yaml", ".yml":
		base := strings.ToLower(path.Base(relativePath))
		return strings.Contains(base, "api") ||
			strings.Contains(base, "schema") ||
			strings.Contains(base, "swagger")
	default:
		return false
	}
}

func detectStructuredAPIContractFormat(relativePath string, body []byte) (gitDocumentationFormat, bool) {
	format, ok := apiContractFormatForPath(relativePath)
	if ok {
		return format, true
	}
	root, err := parseAPIContractMap(body)
	if err != nil {
		return gitDocumentationFormat{}, false
	}
	switch {
	case apiContractStringValue(root["openapi"]) != "":
		return gitDocumentationFormat{format: "openapi", language: apiContractLanguage(path.Ext(relativePath))}, true
	case apiContractStringValue(root["swagger"]) != "":
		return gitDocumentationFormat{format: "swagger", language: apiContractLanguage(path.Ext(relativePath))}, true
	case apiContractStringValue(root["asyncapi"]) != "":
		return gitDocumentationFormat{format: "asyncapi", language: apiContractLanguage(path.Ext(relativePath))}, true
	default:
		return gitDocumentationFormat{}, false
	}
}

func apiContractLanguage(ext string) string {
	if strings.EqualFold(ext, ".json") {
		return "json"
	}
	return "yaml"
}

func extractAPIContractDocumentation(
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	body []byte,
	formatName string,
) (facts.DocumentationDocumentPayload, []facts.DocumentationSectionPayload, []facts.DocumentationLinkPayload) {
	if formatName == "graphql_sdl" {
		return extractGraphQLSDLDocumentation(repo, relativePath, digest, commitSHA, body)
	}
	bodyText, warnings := boundedDocumentationBodyBytes(body, apiContractMaxBodyBytes)
	root, err := parseAPIContractMap([]byte(bodyText))
	if err != nil {
		warnings = append(warnings, "malformed_api_contract")
		document := apiContractDocumentPayload(repo, relativePath, digest, commitSHA, bodyText, formatName, warnings, nil)
		return document, nil, nil
	}
	formatName = apiContractFormatFromRoot(root, formatName)
	drafts, links := apiContractSectionDrafts(root, formatName)
	if len(drafts) > apiContractMaxSections {
		drafts = drafts[:apiContractMaxSections]
		warnings = append(warnings, "section_limit_exceeded")
	}
	documentID := gitDocumentationDocumentID(repo.ID, relativePath)
	revisionID := firstNonEmptyString(commitSHA, digest, "unknown")
	sections := documentationSectionsFromDrafts(documentID, revisionID, relativePath, formatName, drafts)
	document := apiContractDocumentPayload(repo, relativePath, digest, commitSHA, bodyText, formatName, warnings, sections)
	return document, sections, apiContractLinks(relativePath, documentID, revisionID, links, sections)
}

func apiContractDocumentPayload(
	repo repositoryidentity.Metadata,
	relativePath string,
	digest string,
	commitSHA string,
	bodyText string,
	formatName string,
	warnings []string,
	sections []facts.DocumentationSectionPayload,
) facts.DocumentationDocumentPayload {
	title := documentationTitle(relativePath, sections)
	document := facts.DocumentationDocumentPayload{
		SourceID:     gitDocumentationSourceID(repo.ID),
		DocumentID:   gitDocumentationDocumentID(repo.ID, relativePath),
		ExternalID:   relativePath,
		RevisionID:   firstNonEmptyString(commitSHA, digest, "unknown"),
		CanonicalURI: gitDocumentationCanonicalURI(repo, relativePath, commitSHA),
		Title:        title,
		DocumentType: "api_contract",
		Format:       formatName,
		Language:     "en",
		ContentHash:  firstNonEmptyString(digest, documentationHashText(bodyText)),
		SourceMetadata: map[string]string{
			"path":    relativePath,
			"repo_id": repo.ID,
		},
	}
	if commitSHA != "" {
		document.SourceMetadata["source_revision"] = commitSHA
	}
	addDocumentationWarnings(document.SourceMetadata, warnings...)
	return document
}

func parseAPIContractMap(body []byte) (map[string]any, error) {
	var root map[string]any
	if json.Valid(body) {
		if err := json.Unmarshal(body, &root); err != nil {
			return nil, err
		}
		return root, nil
	}
	if err := yaml.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	return root, nil
}

func apiContractFormatFromRoot(root map[string]any, fallback string) string {
	switch {
	case apiContractStringValue(root["openapi"]) != "":
		return "openapi"
	case apiContractStringValue(root["swagger"]) != "":
		return "swagger"
	case apiContractStringValue(root["asyncapi"]) != "":
		return "asyncapi"
	default:
		return fallback
	}
}

func apiContractSectionDrafts(root map[string]any, formatName string) ([]markdownSectionDraft, []apiContractLinkDraft) {
	switch formatName {
	case "asyncapi":
		return asyncAPISectionDrafts(root)
	default:
		return openAPISectionDrafts(root, formatName)
	}
}

func openAPISectionDrafts(root map[string]any, formatName string) ([]markdownSectionDraft, []apiContractLinkDraft) {
	drafts := []markdownSectionDraft{}
	links := apiContractExternalLinks(root)
	for _, apiPath := range sortedMapKeys(apiContractMapValue(root["paths"])) {
		pathMap := apiContractMapValue(root["paths"])[apiPath]
		for _, method := range sortedOpenAPIMethods(apiContractMapValue(pathMap)) {
			operation := apiContractMapValue(apiContractMapValue(pathMap)[method])
			heading := strings.ToUpper(method) + " " + apiPath
			pointer := "/paths/" + jsonPointerEscape(apiPath) + "/" + method
			drafts = append(drafts, apiContractDraft(
				1,
				heading,
				"operation-"+method+"-"+apiPathAnchor(apiPath),
				pointer,
				map[string]string{"contract_kind": formatName, "operation_path": apiPath, "method": method},
				apiOperationContent(operation),
			))
		}
	}
	schemaRoot := apiContractMapValue(apiContractMapValue(root["components"])["schemas"])
	schemaBasePointer := "/components/schemas/"
	if formatName == "swagger" {
		schemaRoot = apiContractMapValue(root["definitions"])
		schemaBasePointer = "/definitions/"
	}
	for _, schemaName := range sortedMapKeys(schemaRoot) {
		schema := apiContractMapValue(schemaRoot[schemaName])
		drafts = append(drafts, apiContractDraft(
			1,
			"Schema "+schemaName,
			"schema-"+markdownAnchor(schemaName),
			schemaBasePointer+jsonPointerEscape(schemaName),
			map[string]string{"contract_kind": formatName, "schema_name": schemaName},
			apiSchemaContent(schema),
		))
	}
	return drafts, links
}

func asyncAPISectionDrafts(root map[string]any) ([]markdownSectionDraft, []apiContractLinkDraft) {
	drafts := []markdownSectionDraft{}
	links := apiContractExternalLinks(root)
	channels := apiContractMapValue(root["channels"])
	for _, channel := range sortedMapKeys(channels) {
		channelMap := apiContractMapValue(channels[channel])
		for _, action := range sortedAsyncAPIActions(channelMap) {
			operation := apiContractMapValue(channelMap[action])
			heading := strings.ToUpper(action) + " " + channel
			pointer := "/channels/" + jsonPointerEscape(channel) + "/" + action
			drafts = append(drafts, apiContractDraft(
				1,
				heading,
				"operation-"+action+"-"+apiPathAnchor(channel),
				pointer,
				map[string]string{"contract_kind": "asyncapi", "channel": channel, "operation": action},
				apiOperationContent(operation),
			))
		}
	}
	for _, schemaName := range sortedMapKeys(apiContractMapValue(apiContractMapValue(root["components"])["schemas"])) {
		schema := apiContractMapValue(apiContractMapValue(apiContractMapValue(root["components"])["schemas"])[schemaName])
		drafts = append(drafts, apiContractDraft(
			1,
			"Schema "+schemaName,
			"schema-"+markdownAnchor(schemaName),
			"/components/schemas/"+jsonPointerEscape(schemaName),
			map[string]string{"contract_kind": "asyncapi", "schema_name": schemaName},
			apiSchemaContent(schema),
		))
	}
	return drafts, links
}

func apiContractDraft(level int, heading string, anchor string, sourceRef string, metadata map[string]string, content []string) markdownSectionDraft {
	return markdownSectionDraft{
		level:          level,
		heading:        heading,
		anchor:         anchor,
		startRef:       sourceRef,
		endRef:         sourceRef,
		content:        content,
		sourceMetadata: metadata,
	}
}

func apiOperationContent(operation map[string]any) []string {
	lines := []string{}
	for _, key := range []string{"operationId", "summary", "description"} {
		if value := apiContractStringValue(operation[key]); value != "" {
			lines = append(lines, value)
		}
	}
	if tags := apiContractStringSliceValue(operation["tags"]); len(tags) > 0 {
		lines = append(lines, "Tags: "+strings.Join(tags, ", "))
	}
	if refs := collectJSONRefs(operation); len(refs) > 0 {
		lines = append(lines, "Schema refs: "+strings.Join(refs, ", "))
	}
	return lines
}

func apiSchemaContent(schema map[string]any) []string {
	lines := []string{}
	if description := apiContractStringValue(schema["description"]); description != "" {
		lines = append(lines, description)
	}
	if schemaType := apiContractStringValue(schema["type"]); schemaType != "" {
		lines = append(lines, "Type: "+schemaType)
	}
	return lines
}

func collectJSONRefs(value any) []string {
	refs := map[string]bool{}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			if ref := apiContractStringValue(typed["$ref"]); ref != "" {
				refs[ref] = true
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	out := make([]string, 0, len(refs))
	for ref := range refs {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

type apiContractLinkDraft struct {
	target string
	text   string
}

func apiContractExternalLinks(root map[string]any) []apiContractLinkDraft {
	externalDocs := apiContractMapValue(root["externalDocs"])
	if target := apiContractStringValue(externalDocs["url"]); target != "" {
		return []apiContractLinkDraft{{target: target, text: firstNonEmptyString(apiContractStringValue(externalDocs["description"]), "externalDocs")}}
	}
	return nil
}

func apiContractLinks(
	relativePath string,
	documentID string,
	revisionID string,
	drafts []apiContractLinkDraft,
	sections []facts.DocumentationSectionPayload,
) []facts.DocumentationLinkPayload {
	if len(drafts) == 0 || len(sections) == 0 {
		return nil
	}
	sectionID := sections[0].SectionID
	links := make([]facts.DocumentationLinkPayload, 0, len(drafts))
	for i, draft := range drafts {
		links = append(links, facts.DocumentationLinkPayload{
			DocumentID:     documentID,
			RevisionID:     revisionID,
			SectionID:      sectionID,
			LinkID:         fmt.Sprintf("link:%s:api-contract:%d", sectionID, i+1),
			TargetURI:      draft.target,
			TargetKind:     documentationLinkTargetKind(draft.target),
			AnchorTextHash: documentationHashText(draft.text),
			SourceMetadata: map[string]string{"path": relativePath},
		})
	}
	return links
}

func sortedOpenAPIMethods(pathMap map[string]any) []string {
	methods := []string{}
	for _, method := range []string{"get", "put", "post", "delete", "patch", "options", "head", "trace"} {
		if _, ok := pathMap[method]; ok {
			methods = append(methods, method)
		}
	}
	return methods
}

func sortedAsyncAPIActions(channelMap map[string]any) []string {
	actions := []string{}
	for _, action := range []string{"publish", "subscribe"} {
		if _, ok := channelMap[action]; ok {
			actions = append(actions, action)
		}
	}
	return actions
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func apiContractMapValue(value any) map[string]any {
	typed, ok := value.(map[string]any)
	if ok {
		return typed
	}
	return nil
}

func apiContractStringValue(value any) string {
	typed, ok := value.(string)
	if ok {
		return strings.TrimSpace(typed)
	}
	return ""
}

func apiContractStringSliceValue(value any) []string {
	typed, ok := value.([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, item := range typed {
		if text := apiContractStringValue(item); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func apiPathAnchor(value string) string {
	return markdownAnchor(strings.Trim(value, "/"))
}

func jsonPointerEscape(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}
