// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	remoteValidationTierDeployed = "deployed_services"
	remoteValidationExitSuccess  = 0
	goldenCorpusEvidenceSource   = "scripts/verify-golden-corpus-gate.sh"
)

var (
	directExitCapturePattern     = regexp.MustCompile(`;\s*echo\s+\$\?\s*$`)
	environmentAssignmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=.*$`)
)

type remoteValidationProofFile struct {
	SchemaVersion int                          `yaml:"schema_version"`
	Proofs        []remoteValidationProofEntry `yaml:"proofs"`
}

type remoteValidationProofEntry struct {
	Slug          string   `yaml:"slug"`
	Tier          string   `yaml:"tier"`
	Date          string   `yaml:"date"`
	Kind          string   `yaml:"kind"`
	Source        string   `yaml:"source"`
	Command       string   `yaml:"command"`
	ExitCode      *int     `yaml:"exit_code"`
	Assertions    []string `yaml:"assertions"`
	B12Assertions []string `yaml:"b12_assertions"`
}

type remoteValidationArtifact struct {
	slug          string
	tier          string
	date          string
	kind          string
	source        string
	command       string
	exitCode      *int
	assertions    []string
	b12Assertions []string
}

func loadRemoteValidationProofCatalog(repoRoot string) (map[string]remoteValidationArtifact, error) {
	path := filepath.Join(repoRoot, filepath.FromSlash(RemoteValidationProofsPath))
	raw, err := os.ReadFile(path) // #nosec G304 -- fixed repository-relative proof catalog path
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]remoteValidationArtifact{}, nil
		}
		return nil, fmt.Errorf("read remote-validation proofs %s: %w", RemoteValidationProofsPath, err)
	}
	var doc remoteValidationProofFile
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", RemoteValidationProofsPath, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse %s: multiple YAML documents are not allowed", RemoteValidationProofsPath)
	}
	if doc.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s schema_version is %d, want 1", RemoteValidationProofsPath, doc.SchemaVersion)
	}
	catalog := make(map[string]remoteValidationArtifact, len(doc.Proofs))
	for i, entry := range doc.Proofs {
		slug := strings.TrimSpace(entry.Slug)
		if slug == "" {
			return nil, fmt.Errorf("%s proofs[%d] is missing slug", RemoteValidationProofsPath, i)
		}
		if !remoteValidationRefValid(slug) {
			return nil, fmt.Errorf("%s proofs[%d] has invalid slug %q", RemoteValidationProofsPath, i, slug)
		}
		if _, exists := catalog[slug]; exists {
			return nil, fmt.Errorf("%s has duplicate proof entry for slug %q", RemoteValidationProofsPath, slug)
		}
		catalog[slug] = remoteValidationArtifact{
			slug:          slug,
			tier:          strings.TrimSpace(entry.Tier),
			date:          strings.TrimSpace(entry.Date),
			kind:          strings.TrimSpace(entry.Kind),
			source:        strings.TrimSpace(entry.Source),
			command:       strings.TrimSpace(entry.Command),
			exitCode:      entry.ExitCode,
			assertions:    trimStringsCopy(entry.Assertions),
			b12Assertions: trimStringsCopy(entry.B12Assertions),
		}
	}
	return catalog, nil
}

func trimStringsCopy(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strings.TrimSpace(value))
	}
	return out
}

// validateRemoteValidationArtifact checks that ref has current-format,
// deployed-tier evidence for every production capability that cites it.
func validateRemoteValidationArtifact(
	repoRoot string,
	catalog map[string]remoteValidationArtifact,
	catalogErr error,
	shapeRefs map[string]b12QueryShapeProof,
	shapeRefsErr error,
	ref string,
	capabilities []string,
) error {
	if !remoteValidationRefValid(ref) {
		return fmt.Errorf("invalid remote-validation slug")
	}
	if catalogErr != nil {
		return catalogErr
	}
	artifact, ok := catalog[ref]
	if !ok {
		return fmt.Errorf("proof entry is missing")
	}
	if err := validateRequiredArtifactFields(artifact); err != nil {
		return err
	}
	if artifact.tier != remoteValidationTierDeployed {
		return fmt.Errorf("tier is %q, want %q", artifact.tier, remoteValidationTierDeployed)
	}
	if err := validateRemoteValidationDate(artifact.date); err != nil {
		return err
	}
	if !directExitCapturePattern.MatchString(artifact.command) {
		return fmt.Errorf("command must end with direct '; echo $?' exit capture")
	}
	if *artifact.exitCode != remoteValidationExitSuccess {
		return fmt.Errorf("exit_code is %d, want 0", *artifact.exitCode)
	}
	if err := validateRemoteValidationSource(repoRoot, artifact.kind, artifact.source); err != nil {
		return err
	}
	if err := validateRemoteValidationCommand(artifact.command, artifact.source); err != nil {
		return err
	}
	if missing := missingCapabilityAssertions(capabilities, artifact.assertions); len(missing) > 0 {
		return fmt.Errorf("missing assertion for %s", strings.Join(missing, ", "))
	}
	if shapeRefsErr != nil {
		return shapeRefsErr
	}
	if err := validateB12Assertions(
		capabilities,
		artifact.b12Assertions,
		shapeRefs,
		artifact.source == goldenCorpusEvidenceSource,
	); err != nil {
		return err
	}
	return nil
}

func validateRequiredArtifactFields(artifact remoteValidationArtifact) error {
	required := []struct {
		name  string
		value string
	}{
		{name: "slug", value: artifact.slug},
		{name: "tier", value: artifact.tier},
		{name: "date", value: artifact.date},
		{name: "kind", value: artifact.kind},
		{name: "source", value: artifact.source},
		{name: "command", value: artifact.command},
	}
	for _, field := range required {
		if field.value == "" {
			return fmt.Errorf("missing %s field", field.name)
		}
	}
	if artifact.exitCode == nil {
		return fmt.Errorf("missing exit_code field")
	}
	if len(artifact.assertions) == 0 {
		return fmt.Errorf("missing assertions field")
	}
	for _, assertion := range artifact.assertions {
		if assertion == "" {
			return fmt.Errorf("assertion must not be empty")
		}
	}
	for _, assertion := range artifact.b12Assertions {
		if assertion == "" {
			return fmt.Errorf("b12_assertion must not be empty")
		}
	}
	return nil
}

func validateB12Assertions(
	capabilities,
	assertions []string,
	shapeRefs map[string]b12QueryShapeProof,
	requireSuccessfulResponse bool,
) error {
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
		shape, ok := shapeRefs[queryRef]
		if !ok {
			return fmt.Errorf("B12-Assertion query shape %q is not found in the committed B-12 snapshot", queryRef)
		}
		// A golden-corpus artifact cites this B-12 response as its deployed
		// capability proof, so an expected refusal cannot substantiate the claim.
		// A dedicated deployed driver may still map the generic stack's default
		// unavailable posture here; its positive proof lives in that driver's
		// separately captured response instead.
		if requireSuccessfulResponse && strings.TrimSpace(shape.ExpectedErrorContains) != "" {
			return fmt.Errorf("B12-Assertion query shape %q is refusal-only proof; a production-supported capability requires a successful deployed response", queryRef)
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

type b12QueryShapeProof struct {
	ExpectedErrorContains string `json:"expected_error_contains"`
}

func loadB12QueryShapeRefs(repoRoot string) (map[string]b12QueryShapeProof, error) {
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
	refs := make(map[string]b12QueryShapeProof, len(snapshot.QueryShapes.MCP)+len(snapshot.QueryShapes.HTTP)+len(snapshot.QueryShapes.CLI))
	for key, raw := range snapshot.QueryShapes.MCP {
		var proof b12QueryShapeProof
		if err := json.Unmarshal(raw, &proof); err != nil {
			return nil, fmt.Errorf("parse committed B-12 MCP query shape %q: %w", key, err)
		}
		refs["mcp:"+key] = proof
	}
	for key, raw := range snapshot.QueryShapes.HTTP {
		var proof b12QueryShapeProof
		if err := json.Unmarshal(raw, &proof); err != nil {
			return nil, fmt.Errorf("parse committed B-12 HTTP query shape %q: %w", key, err)
		}
		refs["http:"+key] = proof
	}
	for key, raw := range snapshot.QueryShapes.CLI {
		var proof b12QueryShapeProof
		if err := json.Unmarshal(raw, &proof); err != nil {
			return nil, fmt.Errorf("parse committed B-12 CLI query shape %q: %w", key, err)
		}
		refs["cli:"+key] = proof
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

func validateRemoteValidationDate(value string) error {
	date, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return fmt.Errorf("date %q must use YYYY-MM-DD", value)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if date.After(today) {
		return fmt.Errorf("date %q is in the future", value)
	}
	return nil
}

func validateRemoteValidationSource(repoRoot, kind, source string) error {
	if filepath.IsAbs(source) || filepath.Clean(source) != source || strings.HasPrefix(source, ".."+string(filepath.Separator)) {
		return fmt.Errorf("source %q must be a clean repository-relative path", source)
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
	default:
		return fmt.Errorf("kind is %q, want compose_e2e or deployed_e2e", kind)
	}
	if !allowed {
		return fmt.Errorf("source %q is not allowed for kind %q", source, kind)
	}
	path := filepath.Join(repoRoot, filepath.FromSlash(source))
	info, err := os.Lstat(path) // #nosec G304 -- source is confined to repository-relative scripts/*.sh paths
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("source %q does not resolve to a committed file", source)
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
