package query

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
)

const cloudFormationRepoFileLimit = 5000

func buildOutgoingCloudFormationRelationships(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
) ([]map[string]any, bool, error) {
	if entity.EntityType != "CloudFormationResource" {
		return nil, false, nil
	}

	templateURL, ok := metadataNonEmptyString(entity.Metadata, "template_url")
	if !ok {
		return nil, true, nil
	}

	targetName := templateURL
	reason := "cloudformation_nested_stack_template"
	if localPath, err := resolveCloudFormationLocalTemplatePath(ctx, reader, entity, templateURL); err != nil {
		return nil, true, err
	} else if localPath != "" {
		targetName = localPath
		reason = "cloudformation_nested_stack_template_local"
	}

	return []map[string]any{{
		"type":        "DEPLOYS_FROM",
		"target_name": targetName,
		"reason":      reason,
	}}, true, nil
}

func resolveCloudFormationLocalTemplatePath(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
	templateURL string,
) (string, error) {
	if reader == nil {
		return "", nil
	}

	candidates := cloudFormationTemplatePathCandidates(entity.RelativePath, templateURL)
	if len(candidates) == 0 {
		return "", nil
	}

	files, err := reader.ListRepoFiles(ctx, entity.RepoID, cloudFormationRepoFileLimit)
	if err != nil {
		return "", fmt.Errorf("list repo files for nested stack resolution: %w", err)
	}

	bestMatch := ""
	bestScore := -1
	for _, file := range files {
		score := cloudFormationTemplateMatchScore(file.RelativePath, candidates)
		if score > bestScore {
			bestScore = score
			bestMatch = file.RelativePath
		}
	}
	if bestScore <= 0 {
		return "", nil
	}
	return bestMatch, nil
}

func cloudFormationTemplatePathCandidates(entityPath string, templateURL string) []string {
	candidates := make([]string, 0, 4)
	addCandidate := func(raw string) {
		cleaned := strings.TrimSpace(path.Clean(raw))
		cleaned = strings.TrimPrefix(cleaned, "./")
		cleaned = strings.TrimPrefix(cleaned, "/")
		if cleaned == "" || cleaned == "." || cleaned == "/" {
			return
		}
		for _, existing := range candidates {
			if existing == cleaned {
				return
			}
		}
		candidates = append(candidates, cleaned)
	}

	addCandidate(templateURL)
	if parsed, err := url.Parse(templateURL); err == nil {
		addCandidate(parsed.Path)
	}
	if dir := path.Dir(strings.ReplaceAll(entityPath, "\\", "/")); dir != "" && dir != "." {
		if parsed, err := url.Parse(templateURL); err == nil && parsed.Scheme == "" && parsed.Host == "" {
			addCandidate(path.Join(dir, templateURL))
		}
	}

	return candidates
}

func cloudFormationTemplateMatchScore(relativePath string, candidates []string) int {
	normalized := strings.TrimPrefix(strings.ReplaceAll(relativePath, "\\", "/"), "./")
	for _, candidate := range candidates {
		if normalized == candidate {
			return len(candidate) + 1000
		}
		if strings.HasSuffix(normalized, "/"+candidate) {
			return len(candidate)
		}
	}
	return -1
}
