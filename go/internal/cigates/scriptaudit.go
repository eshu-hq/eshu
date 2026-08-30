// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ScriptStatus is the strongest repository evidence attached to a tracked
// shell script. It is deliberately not a deletion recommendation.
type ScriptStatus string

const (
	// ScriptStatusGateEntrypoint means a registry local command names the script.
	ScriptStatusGateEntrypoint ScriptStatus = "gate-entrypoint"
	// ScriptStatusReferenced means repository evidence names the script, but no
	// registry local command uses it as an entrypoint.
	ScriptStatusReferenced ScriptStatus = "referenced"
	// ScriptStatusUnreferenced means no supported in-repository usage reference
	// was observed. Gate triggers are selection coverage, not usage. Manual or
	// external callers can still rely on the script.
	ScriptStatusUnreferenced ScriptStatus = "unreferenced"
)

// ReferenceKind describes what a tracked file's exact script-path reference proves.
type ReferenceKind string

const (
	// ReferenceLiteralSource is a literal shell source or dot edge.
	ReferenceLiteralSource ReferenceKind = "literal-source"
	// ReferenceLiteralMention is an exact path mention with no execution claim.
	ReferenceLiteralMention ReferenceKind = "literal-mention"
)

// GateCommandEvidence identifies the registry field naming a script.
type GateCommandEvidence struct {
	GateID string `json:"gate_id"`
	Field  string `json:"field"`
}

// WorkflowRunEvidence identifies a workflow job whose run block names a script.
type WorkflowRunEvidence struct {
	Workflow string `json:"workflow"`
	Job      string `json:"job"`
}

// ScriptReference identifies a tracked file that names a script exactly.
type ScriptReference struct {
	Source string        `json:"source"`
	Kind   ReferenceKind `json:"kind"`
}

// ScriptAudit records repository evidence for one tracked .sh file.
type ScriptAudit struct {
	Path         string                `json:"path"`
	Status       ScriptStatus          `json:"status"`
	GateCommands []GateCommandEvidence `json:"gate_commands,omitempty"`
	GateTriggers []string              `json:"gate_triggers,omitempty"`
	WorkflowRuns []WorkflowRunEvidence `json:"workflow_runs,omitempty"`
	References   []ScriptReference     `json:"references,omitempty"`
}

var scriptPathTokenRE = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*\.sh`)

// AuditScripts reports deterministic ownership and reference evidence for
// every tracked regular .sh file present in the repoRoot working tree.
// Unreferenced results are advisory: they do not prove that a maintainer or
// external operator does not invoke a script directly. A tracked path
// intentionally deleted from the working tree, a symlink, or another
// non-regular path is omitted; any other incomplete file or workflow read
// fails instead of returning an authoritative-looking partial report.
func AuditScripts(repoRoot string, reg *Registry) ([]ScriptAudit, error) {
	tracked, err := loadTrackedPaths(repoRoot)
	if err != nil {
		return nil, err
	}
	paths := trackedPathStrings(tracked)
	scripts := make(map[string]*ScriptAudit)
	for _, path := range paths {
		if strings.HasSuffix(path, ".sh") {
			scripts[path] = &ScriptAudit{Path: path}
		}
	}

	attachGateEvidence(reg, scripts)
	if err := attachFileEvidence(repoRoot, paths, scripts); err != nil {
		return nil, err
	}
	return finalizeScriptAudits(scripts), nil
}

func attachGateEvidence(reg *Registry, scripts map[string]*ScriptAudit) {
	for _, gate := range reg.Gates {
		for path, entry := range scripts {
			if anyTriggerMatches(gate.Triggers, path) {
				entry.GateTriggers = appendUniqueString(entry.GateTriggers, gate.ID)
			}
		}
		if gate.Local == nil {
			continue
		}
		commands := []struct{ field, command string }{
			{"local.command", gate.Local.Command},
			{"local.test_command", gate.Local.TestCommand},
		}
		for _, command := range commands {
			for _, path := range gateCommandScriptTokens(command.command, scripts) {
				evidence := GateCommandEvidence{GateID: gate.ID, Field: command.field}
				scripts[path].GateCommands = appendUniqueGateCommand(scripts[path].GateCommands, evidence)
			}
		}
	}
}

func attachFileEvidence(repoRoot string, paths []string, scripts map[string]*ScriptAudit) error {
	for _, referrer := range paths {
		raw, regular, err := readTrackedRegularFile(repoRoot, referrer)
		if err != nil {
			return err
		}
		if !regular {
			delete(scripts, referrer)
			continue
		}
		// Registry declarations are already represented by GateCommands and
		// GateTriggers. Counting their literal YAML text again as an inbound
		// reference would turn trigger-only coverage into apparent usage.
		if referrer == "specs/ci-gates.v1.yaml" {
			continue
		}
		sourced := make(map[string]struct{})
		for _, path := range sourcedScripts(repoRoot, referrer) {
			sourced[path] = struct{}{}
		}
		for _, path := range scriptTokens(string(raw), scripts) {
			if path == referrer {
				continue
			}
			kind := ReferenceLiteralMention
			if _, ok := sourced[path]; ok {
				kind = ReferenceLiteralSource
			}
			ref := ScriptReference{Source: referrer, Kind: kind}
			scripts[path].References = appendUniqueReference(scripts[path].References, ref)
		}
		if isWorkflowPath(referrer) {
			if err := attachWorkflowEvidence(referrer, raw, scripts); err != nil {
				return err
			}
		}
	}
	return nil
}

func attachWorkflowEvidence(path string, raw []byte, scripts map[string]*ScriptAudit) error {
	var workflow runWorkflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return fmt.Errorf("parse tracked workflow %q: %w", path, err)
	}
	for jobID, job := range workflow.Jobs {
		for _, step := range job.Steps {
			workingDirectory := workflow.Defaults.Run.WorkingDirectory
			if job.Defaults.Run.WorkingDirectory != "" {
				workingDirectory = job.Defaults.Run.WorkingDirectory
			}
			if step.WorkingDirectory != "" {
				workingDirectory = step.WorkingDirectory
			}
			for _, script := range scriptTokensAt(step.Run, workingDirectory, scripts) {
				run := WorkflowRunEvidence{Workflow: path, Job: jobID}
				scripts[script].WorkflowRuns = appendUniqueWorkflowRun(scripts[script].WorkflowRuns, run)
			}
		}
	}
	return nil
}

func readTrackedRegularFile(repoRoot, path string) ([]byte, bool, error) {
	full := filepath.Join(repoRoot, filepath.FromSlash(path))
	info, err := os.Lstat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("inspect tracked file %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, false, nil
	}
	raw, err := os.ReadFile(full) // #nosec G304 -- path comes from git ls-files under repoRoot
	if err != nil {
		return nil, false, fmt.Errorf("read tracked file %q: %w", path, err)
	}
	return raw, true, nil
}

func finalizeScriptAudits(scripts map[string]*ScriptAudit) []ScriptAudit {
	result := make([]ScriptAudit, 0, len(scripts))
	for _, entry := range scripts {
		sort.Slice(entry.GateCommands, func(i, j int) bool {
			if entry.GateCommands[i].GateID == entry.GateCommands[j].GateID {
				return entry.GateCommands[i].Field < entry.GateCommands[j].Field
			}
			return entry.GateCommands[i].GateID < entry.GateCommands[j].GateID
		})
		sort.Strings(entry.GateTriggers)
		sort.Slice(entry.WorkflowRuns, func(i, j int) bool {
			if entry.WorkflowRuns[i].Workflow == entry.WorkflowRuns[j].Workflow {
				return entry.WorkflowRuns[i].Job < entry.WorkflowRuns[j].Job
			}
			return entry.WorkflowRuns[i].Workflow < entry.WorkflowRuns[j].Workflow
		})
		sort.Slice(entry.References, func(i, j int) bool {
			if entry.References[i].Source == entry.References[j].Source {
				return entry.References[i].Kind < entry.References[j].Kind
			}
			return entry.References[i].Source < entry.References[j].Source
		})
		switch {
		case len(entry.GateCommands) > 0:
			entry.Status = ScriptStatusGateEntrypoint
		case len(entry.WorkflowRuns) > 0 || len(entry.References) > 0:
			entry.Status = ScriptStatusReferenced
		default:
			entry.Status = ScriptStatusUnreferenced
		}
		result = append(result, *entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func trackedPathStrings(tracked *trackedPaths) []string {
	paths := make([]string, 0, len(tracked.all))
	for _, segments := range tracked.all {
		paths = append(paths, strings.Join(segments, "/"))
	}
	return paths
}

func scriptTokens(raw string, scripts map[string]*ScriptAudit) []string {
	return scriptTokensAt(raw, ".", scripts)
}

func gateCommandScriptTokens(command string, scripts map[string]*ScriptAudit) []string {
	workingDirectory := "."
	fields := strings.Fields(command)
	if len(fields) >= 3 && fields[0] == "cd" && fields[2] == "&&" {
		workingDirectory = strings.Trim(fields[1], `"'`)
	}
	return scriptTokensAt(command, workingDirectory, scripts)
}

func scriptTokensAt(raw, workingDirectory string, scripts map[string]*ScriptAudit) []string {
	var paths []string
	for _, match := range scriptPathTokenRE.FindAllStringIndex(raw, -1) {
		if match[1] < len(raw) && isScriptPathCharacter(raw[match[1]]) {
			continue
		}
		if token, ok := resolveScriptToken(raw[match[0]:match[1]], workingDirectory, scripts); ok {
			paths = appendUniqueString(paths, token)
		}
	}
	return paths
}

func resolveScriptToken(rawToken, workingDirectory string, scripts map[string]*ScriptAudit) (string, bool) {
	normalized := filepath.ToSlash(filepath.Clean(filepath.Join(
		filepath.FromSlash(workingDirectory),
		filepath.FromSlash(rawToken),
	)))
	if _, ok := scripts[normalized]; ok {
		return normalized, true
	}

	// A common shell shape assigns "$REPO_ROOT/scripts/lib/x.sh" to a
	// variable, then sources the variable. The lexical token includes the
	// variable name, but its tracked repo-relative suffix is still exact. Keep
	// that as a literal mention rather than upgrading it to a source edge.
	segments := strings.Split(rawToken, "/")
	if len(segments) < 2 || !strings.HasSuffix(strings.ToLower(segments[0]), "_root") {
		return "", false
	}
	for i := 1; i < len(segments); i++ {
		candidate := strings.Join(segments[i:], "/")
		if _, ok := scripts[candidate]; ok {
			return candidate, true
		}
	}
	return "", false
}

func isScriptPathCharacter(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9') || strings.ContainsRune("_./-", rune(char))
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueGateCommand(values []GateCommandEvidence, value GateCommandEvidence) []GateCommandEvidence {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueWorkflowRun(values []WorkflowRunEvidence, value WorkflowRunEvidence) []WorkflowRunEvidence {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueReference(values []ScriptReference, value ScriptReference) []ScriptReference {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func isWorkflowPath(path string) bool {
	return strings.HasPrefix(path, ".github/workflows/") &&
		(strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml"))
}
