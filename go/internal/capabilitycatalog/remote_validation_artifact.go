// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	remoteValidationTierDeployed = "deployed_services"
	remoteValidationExitSuccess  = 0
)

var (
	directExitCapturePattern     = regexp.MustCompile(`;\s*echo\s+\$\?\s*$`)
	environmentAssignmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=.*$`)
)

type remoteValidationArtifact struct {
	slug          string
	tier          string
	date          string
	kind          string
	source        string
	command       string
	exitCode      string
	assertions    []string
	b12Assertions []string
}

// validateRemoteValidationArtifact checks that ref has current-format,
// deployed-tier evidence for every production capability that cites it.
func validateRemoteValidationArtifact(repoRoot, ref string, capabilities []string) error {
	path, ok := remoteValidationArtifactPath(repoRoot, ref)
	if !ok {
		return fmt.Errorf("invalid remote-validation slug")
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- ref is slug-validated and path is confined below the evidence directory
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("artifact is missing")
		}
		return fmt.Errorf("read artifact: %w", err)
	}
	info, err := os.Stat(path) // #nosec G304 -- same confined path as the read above
	if err != nil || info.IsDir() {
		return fmt.Errorf("artifact is not a regular file")
	}

	artifact, err := parseRemoteValidationArtifact(string(raw))
	if err != nil {
		return err
	}
	if artifact.slug != ref {
		return fmt.Errorf("Validation-Slug is %q, want %q", artifact.slug, ref)
	}
	if artifact.tier != remoteValidationTierDeployed {
		return fmt.Errorf("Validation-Tier is %q, want %q", artifact.tier, remoteValidationTierDeployed)
	}
	if err := validateRemoteValidationDate(artifact.date); err != nil {
		return err
	}
	if !directExitCapturePattern.MatchString(artifact.command) {
		return fmt.Errorf("Validation-Command must end with direct '; echo $?' exit capture")
	}
	exitCode, err := strconv.Atoi(artifact.exitCode)
	if err != nil || exitCode != remoteValidationExitSuccess {
		return fmt.Errorf("Validation-Exit-Code is %q, want 0", artifact.exitCode)
	}
	if err := validateRemoteValidationSource(repoRoot, artifact.kind, artifact.source); err != nil {
		return err
	}
	if artifact.kind == "compose_e2e" || artifact.kind == "deployed_e2e" {
		if err := validateRemoteValidationCommand(artifact.command, artifact.source); err != nil {
			return err
		}
	}
	if missing := missingCapabilityAssertions(capabilities, artifact.assertions); len(missing) > 0 {
		return fmt.Errorf("missing Capability-Assertion for %s", strings.Join(missing, ", "))
	}
	shapeRefs, err := loadB12QueryShapeRefs(repoRoot)
	if err != nil {
		return err
	}
	if err := validateB12Assertions(capabilities, artifact.b12Assertions, shapeRefs); err != nil {
		return err
	}
	return nil
}

func validateB12Assertions(capabilities, assertions []string, shapeRefs map[string]struct{}) error {
	expected := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		expected[capability] = struct{}{}
	}
	seen := make(map[string]struct{}, len(assertions))
	for _, assertion := range assertions {
		capability, queryRef, ok := strings.Cut(assertion, " -> ")
		capability = strings.TrimSpace(capability)
		queryRef = strings.TrimSpace(queryRef)
		if !ok || capability == "" || queryRef == "" {
			return fmt.Errorf("B12-Assertion %q must use '<capability> -> <transport>:<query-shape-key>'", assertion)
		}
		if _, ok := expected[capability]; !ok {
			return fmt.Errorf("B12-Assertion names unexpected capability %q", capability)
		}
		if _, ok := seen[capability]; ok {
			return fmt.Errorf("duplicate B12-Assertion for %s", capability)
		}
		seen[capability] = struct{}{}
		if _, ok := shapeRefs[queryRef]; !ok {
			return fmt.Errorf("B12-Assertion query shape %q is not found in the committed B-12 snapshot", queryRef)
		}
	}
	missing := make([]string, 0, len(expected)-len(seen))
	for capability := range expected {
		if _, ok := seen[capability]; !ok {
			missing = append(missing, capability)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("missing B12-Assertion for %s", strings.Join(missing, ", "))
	}
	return nil
}

func loadB12QueryShapeRefs(repoRoot string) (map[string]struct{}, error) {
	path := filepath.Join(repoRoot, "testdata", "golden", "e2e-20repo-snapshot.json")
	raw, err := os.ReadFile(path) // #nosec G304 -- path is fixed beneath the validated repository root
	if err != nil {
		return nil, fmt.Errorf("read committed B-12 snapshot: %w", err)
	}
	var snapshot struct {
		QueryShapes struct {
			MCP  map[string]json.RawMessage `json:"mcp"`
			HTTP map[string]json.RawMessage `json:"http"`
			CLI  map[string]json.RawMessage `json:"cli"`
		} `json:"query_shapes"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("parse committed B-12 snapshot: %w", err)
	}
	refs := make(map[string]struct{}, len(snapshot.QueryShapes.MCP)+len(snapshot.QueryShapes.HTTP)+len(snapshot.QueryShapes.CLI))
	for key := range snapshot.QueryShapes.MCP {
		refs["mcp:"+key] = struct{}{}
	}
	for key := range snapshot.QueryShapes.HTTP {
		refs["http:"+key] = struct{}{}
	}
	for key := range snapshot.QueryShapes.CLI {
		refs["cli:"+key] = struct{}{}
	}
	return refs, nil
}

// validateRemoteValidationCommand proves that the exit status captured by the
// artifact belongs to one direct invocation of source. It deliberately accepts
// only a simple command: optional environment assignments, optional bash, the
// exact source path, arguments and redirections, then `; echo $?`.
func validateRemoteValidationCommand(command, source string) error {
	exitCapture := directExitCapturePattern.FindStringIndex(command)
	if exitCapture == nil {
		return fmt.Errorf("Validation-Command must end with direct '; echo $?' exit capture")
	}
	words, err := remoteValidationCommandWords(strings.TrimSpace(command[:exitCapture[0]]))
	if err != nil {
		return fmt.Errorf("Validation-Command must directly run Evidence-Source %q: %w", source, err)
	}
	commandIndex := 0
	for commandIndex < len(words) && environmentAssignmentPattern.MatchString(words[commandIndex]) {
		commandIndex++
	}
	if commandIndex < len(words) && words[commandIndex] == "bash" {
		commandIndex++
	}
	if commandIndex >= len(words) || words[commandIndex] != source {
		return fmt.Errorf("Validation-Command must directly run Evidence-Source %q before capturing its exit code", source)
	}
	return nil
}

// remoteValidationCommandWords tokenizes the narrow simple-command grammar
// accepted for evidence capture. Shell control operators are rejected because
// they can replace or mask the driver's exit status.
func remoteValidationCommandWords(command string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote byte
	escaped := false
	flush := func() {
		if word.Len() == 0 {
			return
		}
		words = append(words, word.String())
		word.Reset()
	}

	for index := 0; index < len(command); index++ {
		character := command[index]
		if escaped {
			word.WriteByte(character)
			escaped = false
			continue
		}
		if quote != 0 {
			switch character {
			case '\\':
				if quote == '"' {
					escaped = true
					continue
				}
			case quote:
				quote = 0
				continue
			}
			word.WriteByte(character)
			continue
		}

		switch character {
		case '\\':
			escaped = true
		case '\'', '"':
			quote = character
		case ' ', '\t':
			flush()
		case '\n', '\r', ';', '|', '#':
			return nil, fmt.Errorf("shell control operators are not allowed before exit capture")
		case '&':
			if index == 0 || command[index-1] != '>' || index+1 >= len(command) || command[index+1] < '0' || command[index+1] > '9' {
				return nil, fmt.Errorf("shell control operators are not allowed before exit capture")
			}
			word.WriteByte(character)
		default:
			word.WriteByte(character)
		}
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("unterminated shell escape or quote")
	}
	flush()
	if len(words) == 0 {
		return nil, fmt.Errorf("driver command is empty")
	}
	return words, nil
}

func parseRemoteValidationArtifact(body string) (remoteValidationArtifact, error) {
	var artifact remoteValidationArtifact
	fields := []struct {
		name        string
		destination *string
	}{
		{name: "Validation-Slug", destination: &artifact.slug},
		{name: "Validation-Tier", destination: &artifact.tier},
		{name: "Validation-Date", destination: &artifact.date},
		{name: "Evidence-Kind", destination: &artifact.kind},
		{name: "Evidence-Source", destination: &artifact.source},
		{name: "Validation-Command", destination: &artifact.command},
		{name: "Validation-Exit-Code", destination: &artifact.exitCode},
	}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Capability-Assertion:") {
			assertion := cleanRemoteValidationField(strings.TrimPrefix(line, "Capability-Assertion:"))
			if assertion == "" {
				return remoteValidationArtifact{}, fmt.Errorf("Capability-Assertion must not be empty")
			}
			artifact.assertions = append(artifact.assertions, assertion)
			continue
		}
		if strings.HasPrefix(line, "B12-Assertion:") {
			assertion := cleanRemoteValidationField(strings.TrimPrefix(line, "B12-Assertion:"))
			if assertion == "" {
				return remoteValidationArtifact{}, fmt.Errorf("B12-Assertion must not be empty")
			}
			artifact.b12Assertions = append(artifact.b12Assertions, assertion)
			continue
		}
		for _, field := range fields {
			prefix := field.name + ":"
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			if seen[field.name] {
				return remoteValidationArtifact{}, fmt.Errorf("duplicate %s field", field.name)
			}
			seen[field.name] = true
			*field.destination = cleanRemoteValidationField(strings.TrimPrefix(line, prefix))
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return remoteValidationArtifact{}, fmt.Errorf("scan artifact: %w", err)
	}
	for _, field := range fields {
		if *field.destination == "" {
			return remoteValidationArtifact{}, fmt.Errorf("missing %s field", field.name)
		}
	}
	if len(artifact.assertions) == 0 {
		return remoteValidationArtifact{}, fmt.Errorf("missing Capability-Assertion field")
	}
	return artifact, nil
}

func cleanRemoteValidationField(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '`' && value[len(value)-1] == '`' {
		return strings.TrimSpace(value[1 : len(value)-1])
	}
	return value
}

func validateRemoteValidationDate(value string) error {
	date, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return fmt.Errorf("Validation-Date %q must use YYYY-MM-DD", value)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if date.After(today) {
		return fmt.Errorf("Validation-Date %q is in the future", value)
	}
	return nil
}

func validateRemoteValidationSource(repoRoot, kind, source string) error {
	if filepath.IsAbs(source) || filepath.Clean(source) != source || strings.HasPrefix(source, ".."+string(filepath.Separator)) {
		return fmt.Errorf("Evidence-Source %q must be a clean repository-relative path", source)
	}
	allowed := false
	switch kind {
	case "compose_e2e":
		base := filepath.Base(source)
		allowed = strings.HasPrefix(source, "scripts/") && strings.HasSuffix(base, ".sh") &&
			(strings.HasPrefix(base, "run-remote-e2e-") || strings.HasPrefix(base, "verify-remote-e2e-") ||
				strings.Contains(base, "compose") || base == "verify-golden-corpus-gate.sh")
	case "deployed_e2e":
		base := filepath.Base(source)
		allowed = strings.HasPrefix(source, "scripts/") && strings.HasSuffix(base, ".sh") &&
			(strings.HasPrefix(base, "run-k8s-") || strings.HasPrefix(base, "verify-hosted-") || strings.Contains(base, "e2e"))
	case "live_backend":
		allowed = strings.HasPrefix(source, "docs/internal/evidence/") && strings.HasSuffix(source, ".md")
	default:
		return fmt.Errorf("Evidence-Kind is %q, want compose_e2e, deployed_e2e, or live_backend", kind)
	}
	if !allowed {
		return fmt.Errorf("Evidence-Source %q is not allowed for Evidence-Kind %q", source, kind)
	}
	path := filepath.Join(repoRoot, filepath.FromSlash(source))
	info, err := os.Lstat(path) // #nosec G304 -- source is confined to one of two fixed repository-relative evidence prefixes
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Evidence-Source %q does not resolve to a committed file", source)
	}
	return nil
}

func missingCapabilityAssertions(capabilities, assertions []string) []string {
	missing := make([]string, 0)
	for _, capability := range capabilities {
		found := false
		for _, assertion := range assertions {
			if strings.HasPrefix(assertion, capability+" ") || strings.HasPrefix(assertion, capability+":") {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, capability)
		}
	}
	sort.Strings(missing)
	return missing
}

func remoteValidationArtifactPath(repoRoot, ref string) (string, bool) {
	if !remoteValidationRefValid(ref) {
		return "", false
	}
	dir := filepath.Clean(filepath.Join(repoRoot, RemoteValidationArtifactDir))
	path := filepath.Clean(filepath.Join(dir, ref+".md"))
	if path != dir && !strings.HasPrefix(path, dir+string(filepath.Separator)) {
		return "", false
	}
	return path, true
}
