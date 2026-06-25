// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

type changeImpactOptions struct {
	JSON            bool
	RepoID          string
	DeveloperIntent string
	BaseRef         string
	HeadRef         string
	RepoPath        string
	ChangedPaths    []string
	Changes         []changeImpactFileChange
	Target          string
	TargetType      string
	ServiceName     string
	WorkloadID      string
	ResourceID      string
	ModuleID        string
	Topic           string
	Environment     string
	MaxDepth        int
	Limit           int
	Offset          int
}

type changeImpactFileChange struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	Status  string `json:"status"`
}

type changeImpactEnvelope struct {
	Data  map[string]any     `json:"data"`
	Truth map[string]any     `json:"truth"`
	Error *changeImpactError `json:"error"`
}

type changeImpactError struct {
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

var (
	changeImpactFetch = fetchChangeImpact
	changePlanFetch   = fetchChangePlan
)

func init() {
	rootCmd.AddCommand(newChangeCommand())
}

func newChangeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change",
		Short: "Inspect pre-change impact over Eshu evidence",
	}
	cmd.AddCommand(newChangeImpactCommand())
	cmd.AddCommand(newChangePlanCommand())
	return cmd
}

func newChangeImpactCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "impact",
		Short: "Map a local diff or changed file list to bounded impact evidence",
		Args:  cobra.NoArgs,
		RunE:  runChangeImpact,
	}
	addChangeImpactFlags(cmd)
	return cmd
}

func newChangePlanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Build a read-only developer change plan over bounded impact evidence",
		Args:  cobra.NoArgs,
		RunE:  runChangePlan,
	}
	addChangeImpactFlags(cmd)
	cmd.Flags().String("intent", "", "Optional developer intent used to rank and explain plan actions")
	return cmd
}

func addChangeImpactFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Write the canonical pre-change impact envelope as JSON")
	cmd.Flags().String("repo-id", "", "Repository selector for changed-path lookup")
	cmd.Flags().String("base", "", "Git base ref for local diff derivation")
	cmd.Flags().String("head", "", "Git head ref for local diff derivation")
	cmd.Flags().StringArray("file", nil, "Repo-relative changed file path; repeat for multiple files")
	cmd.Flags().String("repo-path", ".", "Local repository path used to derive --base/--head diffs")
	cmd.Flags().String("target", "", "Optional canonical entity id or exact entity name")
	cmd.Flags().String("target-type", "", "Optional target kind")
	cmd.Flags().String("service-name", "", "Optional service or workload name")
	cmd.Flags().String("workload-id", "", "Optional canonical workload id")
	cmd.Flags().String("resource-id", "", "Optional canonical cloud resource id")
	cmd.Flags().String("module-id", "", "Optional Terraform module id")
	cmd.Flags().String("topic", "", "Optional code topic to scope impact")
	cmd.Flags().String("env", "", "Optional environment filter")
	cmd.Flags().Int("max-depth", 4, "Maximum graph traversal depth (max 8)")
	cmd.Flags().Int("limit", 25, "Maximum rows per response section (max 100)")
	cmd.Flags().Int("offset", 0, "Result offset for content-backed code investigation")
	addRemoteFlags(cmd)
}

func runChangeImpact(cmd *cobra.Command, _ []string) error {
	opts, err := changeImpactOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	if len(opts.Changes) == 0 && (opts.BaseRef != "" || opts.HeadRef != "") {
		changes, err := gitDiffNameStatus(opts.RepoPath, opts.BaseRef, opts.HeadRef)
		if err != nil {
			return err
		}
		opts.Changes = changes
		opts.ChangedPaths = changeImpactPaths(changes)
	}
	if err := validateChangeImpactOptions(opts); err != nil {
		return err
	}

	envelope, err := changeImpactFetch(apiClientFromCmd(cmd), opts)
	if err != nil {
		envelope = changeImpactEnvelope{
			Error: &changeImpactError{Code: traceErrorCodeFromTransport(err), Message: err.Error()},
		}
		return finishChangeImpact(cmd, opts, envelope, changeImpactEnvelopeError(envelope.Error))
	}
	if envelope.Error != nil {
		return finishChangeImpact(cmd, opts, envelope, changeImpactEnvelopeError(envelope.Error))
	}
	if freshness := traceString(traceMap(envelope.Truth, "freshness"), "state"); freshness == "stale" || freshness == "building" {
		return finishChangeImpact(cmd, opts, envelope, commandExitError{
			message: fmt.Sprintf("pre-change impact freshness is %s", freshness),
			code:    4,
		})
	}
	if traceBool(envelope.Data, "truncated") || traceBool(traceMap(envelope.Data, "answer_packet"), "partial") {
		return finishChangeImpact(cmd, opts, envelope, commandExitError{
			message: "pre-change impact is partial or truncated",
			code:    5,
		})
	}
	return finishChangeImpact(cmd, opts, envelope, nil)
}

func changeImpactOptionsFromCommand(cmd *cobra.Command) (changeImpactOptions, error) {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return changeImpactOptions{}, err
	}
	files, err := cmd.Flags().GetStringArray("file")
	if err != nil {
		return changeImpactOptions{}, err
	}
	opts := changeImpactOptions{JSON: jsonOutput, ChangedPaths: cleanStringValues(files), Changes: changeImpactModifiedFiles(files)}
	if opts.RepoID, err = trimmedFlag(cmd, "repo-id"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.BaseRef, err = trimmedFlag(cmd, "base"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.HeadRef, err = trimmedFlag(cmd, "head"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.RepoPath, err = trimmedFlag(cmd, "repo-path"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.Target, err = trimmedFlag(cmd, "target"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.TargetType, err = trimmedFlag(cmd, "target-type"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.ServiceName, err = trimmedFlag(cmd, "service-name"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.WorkloadID, err = trimmedFlag(cmd, "workload-id"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.ResourceID, err = trimmedFlag(cmd, "resource-id"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.ModuleID, err = trimmedFlag(cmd, "module-id"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.Topic, err = trimmedFlag(cmd, "topic"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.Environment, err = trimmedFlag(cmd, "env"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.MaxDepth, err = cmd.Flags().GetInt("max-depth"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.Limit, err = cmd.Flags().GetInt("limit"); err != nil {
		return changeImpactOptions{}, err
	}
	if opts.Offset, err = cmd.Flags().GetInt("offset"); err != nil {
		return changeImpactOptions{}, err
	}
	return opts, nil
}

func trimmedFlag(cmd *cobra.Command, name string) (string, error) {
	value, err := cmd.Flags().GetString(name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func validateChangeImpactOptions(opts changeImpactOptions) error {
	if len(opts.Changes) > 0 && opts.RepoID == "" {
		return commandExitError{message: "--repo-id is required when changed files are provided", code: 2}
	}
	if len(opts.Changes) == 0 && opts.BaseRef == "" && opts.HeadRef == "" && opts.Target == "" &&
		opts.ServiceName == "" && opts.WorkloadID == "" && opts.ResourceID == "" && opts.ModuleID == "" && opts.Topic == "" {
		return commandExitError{message: "--file, --base/--head, --target, --service-name, or --topic is required", code: 2}
	}
	if opts.Limit <= 0 || opts.Limit > 100 {
		return commandExitError{message: "--limit must be between 1 and 100", code: 2}
	}
	if opts.MaxDepth <= 0 || opts.MaxDepth > 8 {
		return commandExitError{message: "--max-depth must be between 1 and 8", code: 2}
	}
	if opts.Offset < 0 {
		return commandExitError{message: "--offset must be greater than or equal to 0", code: 2}
	}
	return nil
}

func fetchChangeImpact(client *APIClient, opts changeImpactOptions) (changeImpactEnvelope, error) {
	body := map[string]any{
		"repo_id":       opts.RepoID,
		"base_ref":      opts.BaseRef,
		"head_ref":      opts.HeadRef,
		"changed_paths": opts.ChangedPaths,
		"changes":       opts.Changes,
		"target":        opts.Target,
		"target_type":   opts.TargetType,
		"service_name":  opts.ServiceName,
		"workload_id":   opts.WorkloadID,
		"resource_id":   opts.ResourceID,
		"module_id":     opts.ModuleID,
		"topic":         opts.Topic,
		"environment":   opts.Environment,
		"max_depth":     opts.MaxDepth,
		"limit":         opts.Limit,
		"offset":        opts.Offset,
	}
	var envelope changeImpactEnvelope
	if err := client.PostEnvelope("/api/v0/impact/pre-change", body, &envelope); err != nil {
		return changeImpactEnvelope{}, err
	}
	return envelope, nil
}

func gitDiffNameStatus(repoPath, baseRef, headRef string) ([]changeImpactFileChange, error) {
	args := []string{"-C", repoPath, "diff", "--name-status", "--find-renames", "--find-copies", "--find-copies-harder"}
	switch {
	case baseRef != "" && headRef != "":
		args = append(args, baseRef, headRef)
	case baseRef != "":
		args = append(args, baseRef)
	case headRef != "":
		args = append(args, headRef)
	}
	args = append(args, "--")
	out, err := exec.Command("git", args...).Output() // #nosec G204 -- fixed binary "git"; args are program-constructed from flag values (refs and "--"), not arbitrary user strings
	if err != nil {
		return nil, fmt.Errorf("derive git diff: %w", err)
	}
	return parseGitNameStatusDiff(string(out)), nil
}

func parseGitNameStatusDiff(output string) []changeImpactFileChange {
	changes := []changeImpactFileChange{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) < 2 {
			continue
		}
		status := normalizeChangeImpactStatus(fields[0])
		change := changeImpactFileChange{Path: fields[1], Status: status}
		if (status == "renamed" || status == "copied") && len(fields) >= 3 {
			change.OldPath = fields[1]
			change.Path = fields[2]
		}
		changes = append(changes, change)
	}
	return changes
}

func normalizeChangeImpactStatus(status string) string {
	status = strings.ToUpper(strings.TrimSpace(status))
	switch {
	case status == "A":
		return "added"
	case status == "D":
		return "deleted"
	case strings.HasPrefix(status, "R"):
		return "renamed"
	case strings.HasPrefix(status, "C"):
		return "copied"
	default:
		return "modified"
	}
}

func changeImpactModifiedFiles(paths []string) []changeImpactFileChange {
	changes := make([]changeImpactFileChange, 0, len(paths))
	for _, value := range cleanStringValues(paths) {
		changes = append(changes, changeImpactFileChange{Path: value, Status: "modified"})
	}
	return changes
}

func changeImpactPaths(changes []changeImpactFileChange) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		if strings.TrimSpace(change.Path) != "" {
			paths = append(paths, change.Path)
		}
	}
	return cleanStringValues(paths)
}

func cleanStringValues(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}

func finishChangeImpact(cmd *cobra.Command, opts changeImpactOptions, envelope changeImpactEnvelope, err error) error {
	if opts.JSON {
		if writeErr := writeTraceJSON(cmd.OutOrStdout(), envelope); writeErr != nil {
			return writeErr
		}
		return err
	}
	if err != nil {
		if envelope.Error != nil {
			if renderErr := renderChangeImpactError(cmd.OutOrStdout(), envelope); renderErr != nil {
				return renderErr
			}
		} else if envelope.Data != nil {
			if renderErr := renderChangeImpactSummary(cmd.OutOrStdout(), envelope); renderErr != nil {
				return renderErr
			}
		}
		return err
	}
	return renderChangeImpactSummary(cmd.OutOrStdout(), envelope)
}

func renderChangeImpactError(w io.Writer, envelope changeImpactEnvelope) error {
	if envelope.Error == nil {
		return nil
	}
	_, err := fmt.Fprintf(w, "Pre-change impact error (%s): %s\n", envelope.Error.Code, envelope.Error.Message)
	return err
}

func renderChangeImpactSummary(w io.Writer, envelope changeImpactEnvelope) error {
	data := envelope.Data
	if freshness := traceString(traceMap(envelope.Truth, "freshness"), "state"); freshness != "" {
		if _, err := fmt.Fprintf(w, "Truth freshness: %s\n", freshness); err != nil {
			return err
		}
	}
	codeSurface := traceMap(data, "code_surface")
	impactSummary := traceMap(data, "impact_summary")
	if _, err := fmt.Fprintf(
		w,
		"Pre-change impact: %d changed files (coverage=%s truncated=%t)\n",
		traceInt(data, "changed_file_count"),
		traceString(traceMap(data, "coverage"), "state"),
		traceBool(data, "truncated"),
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		w,
		"  symbols=%d direct=%d transitive=%d missing_evidence=%d\n",
		traceInt(codeSurface, "symbol_count"),
		traceInt(impactSummary, "direct_count"),
		traceInt(impactSummary, "transitive_count"),
		len(traceSlice(data, "missing_evidence")),
	); err != nil {
		return err
	}
	for _, raw := range traceSlice(data, "recommended_next_calls") {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(w, "  next=%s reason=%s\n", traceString(call, "tool"), traceString(call, "reason")); err != nil {
			return err
		}
	}
	return nil
}

func changeImpactEnvelopeError(e *changeImpactError) error {
	if e == nil {
		return nil
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(e.Code)
	}
	if message == "" {
		message = "pre-change impact failed"
	}
	return commandExitError{message: message, code: traceExitCode(e.Code)}
}
