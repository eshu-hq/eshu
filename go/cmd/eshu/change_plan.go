package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func runChangePlan(cmd *cobra.Command, _ []string) error {
	opts, err := changeImpactOptionsFromCommand(cmd)
	if err != nil {
		return err
	}
	if opts.DeveloperIntent, err = trimmedFlag(cmd, "intent"); err != nil {
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
	envelope, err := changePlanFetch(apiClientFromCmd(cmd), opts)
	if err != nil {
		envelope = changeImpactEnvelope{
			Error: &changeImpactError{Code: traceErrorCodeFromTransport(err), Message: err.Error()},
		}
		return finishChangePlan(cmd, opts, envelope, changeImpactEnvelopeError(envelope.Error))
	}
	if envelope.Error != nil {
		return finishChangePlan(cmd, opts, envelope, changeImpactEnvelopeError(envelope.Error))
	}
	if freshness := traceString(traceMap(envelope.Truth, "freshness"), "state"); freshness == "stale" || freshness == "building" {
		return finishChangePlan(cmd, opts, envelope, commandExitError{
			message: fmt.Sprintf("developer change plan freshness is %s", freshness),
			code:    4,
		})
	}
	if traceBool(envelope.Data, "blocked") || traceBool(envelope.Data, "truncated") || traceBool(traceMap(envelope.Data, "answer_packet"), "partial") {
		return finishChangePlan(cmd, opts, envelope, commandExitError{
			message: "developer change plan is blocked, partial, or truncated",
			code:    5,
		})
	}
	return finishChangePlan(cmd, opts, envelope, nil)
}

func fetchChangePlan(client *APIClient, opts changeImpactOptions) (changeImpactEnvelope, error) {
	body := map[string]any{
		"developer_intent": opts.DeveloperIntent,
		"repo_id":          opts.RepoID,
		"base_ref":         opts.BaseRef,
		"head_ref":         opts.HeadRef,
		"changed_paths":    opts.ChangedPaths,
		"changes":          opts.Changes,
		"target":           opts.Target,
		"target_type":      opts.TargetType,
		"service_name":     opts.ServiceName,
		"workload_id":      opts.WorkloadID,
		"resource_id":      opts.ResourceID,
		"module_id":        opts.ModuleID,
		"topic":            opts.Topic,
		"environment":      opts.Environment,
		"max_depth":        opts.MaxDepth,
		"limit":            opts.Limit,
		"offset":           opts.Offset,
	}
	var envelope changeImpactEnvelope
	if err := client.PostEnvelope("/api/v0/impact/developer-change-plan", body, &envelope); err != nil {
		return changeImpactEnvelope{}, err
	}
	return envelope, nil
}

func finishChangePlan(cmd *cobra.Command, opts changeImpactOptions, envelope changeImpactEnvelope, err error) error {
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
			if renderErr := renderChangePlanSummary(cmd.OutOrStdout(), envelope); renderErr != nil {
				return renderErr
			}
		}
		return err
	}
	return renderChangePlanSummary(cmd.OutOrStdout(), envelope)
}

func renderChangePlanSummary(w io.Writer, envelope changeImpactEnvelope) error {
	data := envelope.Data
	if _, err := fmt.Fprintf(
		w,
		"Developer change plan: %d actions for %d changed files (blocked=%t truncated=%t)\n",
		len(traceSlice(data, "actions")),
		traceInt(data, "changed_file_count"),
		traceBool(data, "blocked"),
		traceBool(data, "truncated"),
	); err != nil {
		return err
	}
	for _, raw := range traceSlice(data, "actions") {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(w, "  action=%s risk=%s title=%s\n", traceString(action, "kind"), traceString(action, "risk"), traceString(action, "title")); err != nil {
			return err
		}
	}
	for _, raw := range traceSlice(data, "bounded_next_calls") {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(w, "  next=%s target=%s\n", traceString(call, "kind"), traceString(call, "target")); err != nil {
			return err
		}
	}
	return nil
}
