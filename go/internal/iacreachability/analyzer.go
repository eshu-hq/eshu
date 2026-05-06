package iacreachability

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Reachability identifies whether modeled content references reach an IaC
// artifact.
type Reachability string

const (
	// ReachabilityUsed means the artifact has a modeled reference.
	ReachabilityUsed Reachability = "used"
	// ReachabilityUnused means no modeled reference reaches the artifact.
	ReachabilityUnused Reachability = "unused"
	// ReachabilityAmbiguous means dynamic content prevents a safe cleanup
	// conclusion.
	ReachabilityAmbiguous Reachability = "ambiguous"
)

// Finding classifies the operator-facing cleanup result for an IaC artifact.
type Finding string

const (
	// FindingInUse marks artifacts that must not be cleanup candidates.
	FindingInUse Finding = "in_use"
	// FindingCandidateDead marks unreferenced artifacts as cleanup candidates.
	FindingCandidateDead Finding = "candidate_dead_iac"
	// FindingAmbiguousDynamic marks dynamic references that need renderer or
	// runtime evidence.
	FindingAmbiguousDynamic Finding = "ambiguous_dynamic_reference"
)

// File is the bounded content input consumed by the reachability analyzer.
type File struct {
	RepoID       string
	RelativePath string
	Content      string
}

// Options controls analyzer scope and whether ambiguous rows are returned.
type Options struct {
	Families         map[string]bool
	IncludeAmbiguous bool
}

// Row is one IaC artifact reachability classification.
type Row struct {
	ID           string
	Family       string
	RepoID       string
	ArtifactPath string
	ArtifactName string
	Reachability Reachability
	Finding      Finding
	Confidence   float64
	Evidence     []string
	Limitations  []string
}

type artifact struct {
	family   string
	repoID   string
	path     string
	name     string
	evidence []string
}

type referenceIndex struct {
	used      map[string][]string
	ambiguous map[string][]string
}

// Analyze classifies IaC artifacts from the provided repository files.
func Analyze(filesByRepo map[string][]File, opts Options) []Row {
	artifacts := discoverArtifacts(filesByRepo, opts.Families)
	refs := buildReferenceIndex(filesByRepo)
	rows := make([]Row, 0, len(artifacts))
	for _, artifact := range artifacts {
		key := artifactKey(artifact.family, artifact.name)
		if evidence := refs.used[key]; len(evidence) > 0 {
			rows = append(rows, newRow(artifact, ReachabilityUsed, FindingInUse, 0.99, evidence))
			continue
		}
		if evidence := refs.ambiguous[key]; len(evidence) > 0 {
			if opts.IncludeAmbiguous {
				rows = append(rows, newRow(artifact, ReachabilityAmbiguous, FindingAmbiguousDynamic, 0.40, evidence))
			}
			continue
		}
		rows = append(rows, newRow(artifact, ReachabilityUnused, FindingCandidateDead, 0.75, artifact.evidence))
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].ID < rows[j].ID
	})
	return rows
}

// CleanupRows filters analyzer rows down to rows that operators may need to
// inspect for cleanup.
func CleanupRows(rows []Row, includeAmbiguous bool) []Row {
	findings := make([]Row, 0, len(rows))
	for _, row := range rows {
		switch row.Reachability {
		case ReachabilityUnused:
			findings = append(findings, row)
		case ReachabilityAmbiguous:
			if includeAmbiguous {
				findings = append(findings, row)
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].ID < findings[j].ID
	})
	return findings
}

// FamilyFilter normalizes request family names for analyzer options.
func FamilyFilter(families []string) map[string]bool {
	if len(families) == 0 {
		return nil
	}
	filter := map[string]bool{}
	for _, family := range families {
		family = strings.ToLower(strings.TrimSpace(family))
		if family != "" {
			filter[family] = true
		}
	}
	return filter
}

// RelevantFile reports whether a content path can define or reference the
// supported IaC families.
func RelevantFile(relativePath string) bool {
	lower := strings.ToLower(relativePath)
	return strings.HasSuffix(lower, ".tf") ||
		strings.HasSuffix(lower, ".hcl") ||
		strings.HasSuffix(lower, ".yaml") ||
		strings.HasSuffix(lower, ".yml") ||
		strings.Contains(lower, "jenkinsfile")
}

func discoverArtifacts(filesByRepo map[string][]File, families map[string]bool) []artifact {
	seen := map[string]struct{}{}
	var artifacts []artifact
	for repoID, files := range filesByRepo {
		for _, file := range files {
			if modulePath, name, ok := terraformModuleArtifact(file.RelativePath); ok && familyEnabled(families, "terraform") {
				artifacts = appendUniqueArtifact(artifacts, seen, artifact{
					family: "terraform", repoID: repoID, path: modulePath, name: name,
					evidence: []string{file.RelativePath + ": module directory exists"},
				})
			}
			if chartPath, name, ok := helmChartArtifact(file.RelativePath); ok && familyEnabled(families, "helm") {
				artifacts = appendUniqueArtifact(artifacts, seen, artifact{
					family: "helm", repoID: repoID, path: chartPath, name: name,
					evidence: []string{file.RelativePath + ": chart metadata exists"},
				})
			}
			if rolePath, name, ok := ansibleRoleArtifact(file.RelativePath); ok && familyEnabled(families, "ansible") {
				artifacts = appendUniqueArtifact(artifacts, seen, artifact{
					family: "ansible", repoID: repoID, path: rolePath, name: name,
					evidence: []string{file.RelativePath + ": role task entrypoint exists"},
				})
			}
			if kustomizePath, name, ok := kustomizeArtifact(file.RelativePath); ok && familyEnabled(families, "kustomize") {
				artifacts = appendUniqueArtifact(artifacts, seen, artifact{
					family: "kustomize", repoID: repoID, path: kustomizePath, name: name,
					evidence: []string{file.RelativePath + ": kustomization entrypoint exists"},
				})
			}
			if familyEnabled(families, "compose") {
				for _, composeArtifact := range composeServiceArtifacts(file) {
					artifacts = appendUniqueArtifact(artifacts, seen, composeArtifact)
				}
			}
		}
	}
	return artifacts
}

func appendUniqueArtifact(artifacts []artifact, seen map[string]struct{}, artifact artifact) []artifact {
	key := artifact.family + ":" + artifact.repoID + ":" + artifact.path
	if _, ok := seen[key]; ok {
		return artifacts
	}
	seen[key] = struct{}{}
	return append(artifacts, artifact)
}

func buildReferenceIndex(filesByRepo map[string][]File) referenceIndex {
	index := referenceIndex{used: map[string][]string{}, ambiguous: map[string][]string{}}
	reachedPlaybooks := collectReachedAnsiblePlaybooks(filesByRepo)
	for _, files := range filesByRepo {
		for _, file := range files {
			if file.Content == "" {
				continue
			}
			recordTerraformReferences(index, file)
			recordHelmReferences(index, file)
			recordAnsibleReferences(index, file, reachedPlaybooks)
			recordKustomizeReferences(index, file)
			recordComposeReferences(index, file)
		}
	}
	return index
}

func newRow(artifact artifact, reachability Reachability, finding Finding, confidence float64, evidence []string) Row {
	limitations := []string(nil)
	if reachability == ReachabilityAmbiguous {
		limitations = []string{"dynamic reference requires renderer or runtime evidence before cleanup"}
	}
	return Row{
		ID:           artifact.family + ":" + artifact.repoID + ":" + artifact.path,
		Family:       artifact.family,
		RepoID:       artifact.repoID,
		ArtifactPath: artifact.path,
		ArtifactName: artifact.name,
		Reachability: reachability,
		Finding:      finding,
		Confidence:   confidence,
		Evidence:     append([]string(nil), evidence...),
		Limitations:  limitations,
	}
}

func familyEnabled(filter map[string]bool, family string) bool {
	return len(filter) == 0 || filter[family]
}

func artifactKey(family, name string) string {
	return family + ":" + strings.ToLower(strings.TrimSpace(name))
}

func addReference(index map[string][]string, family, name string, evidence ...string) {
	name = strings.Trim(strings.TrimSpace(name), `"'`)
	if name == "" {
		return
	}
	key := artifactKey(family, path.Base(name))
	index[key] = append(index[key], evidence...)
}

func terraformModuleArtifact(relativePath string) (string, string, bool) {
	parts := strings.Split(path.Clean(relativePath), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "modules" && parts[i+1] != "" {
			return strings.Join(parts[:i+2], "/"), parts[i+1], true
		}
	}
	return "", "", false
}

func helmChartArtifact(relativePath string) (string, string, bool) {
	clean := path.Clean(relativePath)
	parts := strings.Split(clean, "/")
	for i := 0; i < len(parts)-2; i++ {
		if parts[i] == "charts" && parts[i+1] != "" && parts[i+2] == "Chart.yaml" {
			return strings.Join(parts[:i+2], "/"), parts[i+1], true
		}
	}
	return "", "", false
}

func ansibleRoleArtifact(relativePath string) (string, string, bool) {
	clean := path.Clean(relativePath)
	parts := strings.Split(clean, "/")
	for i := 0; i < len(parts)-3; i++ {
		if parts[i] == "roles" && parts[i+1] != "" && parts[i+2] == "tasks" {
			return strings.Join(parts[:i+2], "/"), parts[i+1], true
		}
	}
	return "", "", false
}

func kustomizeArtifact(relativePath string) (string, string, bool) {
	clean := path.Clean(relativePath)
	fileName := strings.ToLower(path.Base(clean))
	if fileName != "kustomization.yaml" && fileName != "kustomization.yml" {
		return "", "", false
	}
	dir := path.Dir(clean)
	if dir == "." || dir == "/" {
		return "", "", false
	}
	return dir, path.Base(dir), true
}

var (
	terraformSourcePattern = regexp.MustCompile(`(?m)\bsource\s*=\s*["']([^"']+)["']`)
	ansiblePlaybookPattern = regexp.MustCompile(`ansible-playbook\s+([^\s'"]+)`)
)

func recordTerraformReferences(index referenceIndex, file File) {
	for _, match := range terraformSourcePattern.FindAllStringSubmatch(file.Content, -1) {
		source := match[1]
		evidence := file.RelativePath + ": terraform source " + source
		if strings.Contains(source, "${") || strings.Contains(source, "{{") {
			for _, token := range referenceTokens(file.Content) {
				addReference(index.ambiguous, "terraform", token, evidence)
			}
			continue
		}
		if strings.Contains(source, "/modules/") {
			addReference(index.used, "terraform", source, evidence)
		}
	}
}

func recordHelmReferences(index referenceIndex, file File) {
	for _, token := range strings.Fields(file.Content) {
		cleaned := strings.Trim(token, `"'`)
		if !strings.Contains(cleaned, "charts/") {
			continue
		}
		evidence := file.RelativePath + ": helm chart reference " + cleaned
		if strings.Contains(cleaned, "{{") || strings.Contains(cleaned, "${") {
			for _, quoted := range referenceTokens(file.Content) {
				addReference(index.ambiguous, "helm", quoted, evidence)
			}
			continue
		}
		_, after, _ := strings.Cut(cleaned, "charts/")
		addReference(index.used, "helm", after, evidence)
	}
}

func collectReachedAnsiblePlaybooks(filesByRepo map[string][]File) map[string][]string {
	reached := map[string][]string{}
	for _, files := range filesByRepo {
		for _, file := range files {
			for _, match := range ansiblePlaybookPattern.FindAllStringSubmatch(file.Content, -1) {
				playbook := normalizeAnsiblePlaybookPath(match[1])
				reached[playbook] = append(reached[playbook], file.RelativePath+": ansible-playbook "+match[1])
			}
		}
	}
	return reached
}

func recordAnsibleReferences(index referenceIndex, file File, reachedPlaybooks map[string][]string) {
	playbook := normalizeAnsiblePlaybookPath(file.RelativePath)
	controllerEvidence := reachedPlaybooks[playbook]
	if len(controllerEvidence) == 0 {
		return
	}
	if strings.Contains(file.Content, "{{") {
		for _, token := range referenceTokens(file.Content) {
			addReference(index.ambiguous, "ansible", token, append(controllerEvidence, file.RelativePath+": dynamic role reference")...)
		}
		return
	}
	for _, line := range strings.Split(file.Content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			addReference(index.used, "ansible", strings.TrimSpace(strings.TrimPrefix(line, "- ")), append(controllerEvidence, file.RelativePath+": role reference")...)
		}
	}
}

func recordKustomizeReferences(index referenceIndex, file File) {
	for _, line := range strings.Split(file.Content, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "path:"):
			recordKustomizePathReference(index, file, strings.TrimSpace(strings.TrimPrefix(line, "path:")))
		case isKustomizationFile(file.RelativePath) && strings.HasPrefix(line, "- "):
			recordKustomizePathReference(index, file, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
		}
	}
}

func recordKustomizePathReference(index referenceIndex, file File, raw string) {
	cleaned := strings.Trim(strings.TrimSpace(raw), `"'`)
	if cleaned == "" {
		return
	}
	if !looksKustomizeReference(cleaned) && !strings.Contains(cleaned, "{{") && !strings.Contains(cleaned, "${") {
		return
	}
	evidence := file.RelativePath + ": kustomize path reference " + cleaned
	if strings.Contains(cleaned, "{{") || strings.Contains(cleaned, "${") {
		for _, token := range referenceTokens(file.Content) {
			addReference(index.ambiguous, "kustomize", token, evidence)
		}
		return
	}
	addReference(index.used, "kustomize", cleaned, evidence)
}

func isKustomizationFile(relativePath string) bool {
	base := strings.ToLower(path.Base(relativePath))
	return base == "kustomization.yaml" || base == "kustomization.yml"
}

func looksKustomizeReference(value string) bool {
	clean := path.Clean(value)
	return strings.Contains(clean, "/base/") ||
		strings.Contains(clean, "/bases/") ||
		strings.Contains(clean, "/overlays/") ||
		strings.HasPrefix(clean, "base/") ||
		strings.HasPrefix(clean, "bases/") ||
		strings.HasPrefix(clean, "overlays/")
}

func normalizeAnsiblePlaybookPath(raw string) string {
	clean := path.Clean(strings.Trim(raw, `"'`))
	if idx := strings.Index(clean, "playbooks/"); idx >= 0 {
		return clean[idx:]
	}
	return clean
}

func referenceTokens(content string) []string {
	matches := regexp.MustCompile(`[A-Za-z0-9_.-]+`).FindAllString(content, -1)
	tokens := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		token := strings.TrimSpace(match)
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	return tokens
}
