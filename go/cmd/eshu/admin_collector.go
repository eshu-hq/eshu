// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/admin"
)

var adminCollectorCmd = &cobra.Command{
	Use:   "collector",
	Short: "Collector administration",
}

func init() {
	adminCmd.AddCommand(adminCollectorCmd)
	adminCollectorCmd.AddCommand(newAdminCollectorAcknowledgeFailureCmd())
}

// newAdminCollectorAcknowledgeFailureCmd builds
// `eshu admin collector acknowledge-failure` (#8830).
func newAdminCollectorAcknowledgeFailureCmd() *cobra.Command {
	ackCmd := &cobra.Command{
		Use:   "acknowledge-failure",
		Short: "Acknowledge a collector terminal failure as known and expected",
		Long: "Marks the newest unresolved terminal failure of one collector instance and scope as known and expected. " +
			"Collector readiness then reports the instance as not failed, with last_failure_acknowledged set, " +
			"until a failure of another class or a failure after a completed item appears. " +
			"Read --instance, --scope, and --failure-class from GET /api/v0/status/collector-readiness or " +
			"GET /api/v0/status/collectors, or the get_collector_readiness and list_collectors MCP tools " +
			"(instance_id, last_failure_scope_id, last_failure_class).",
		RunE: runAdminCollectorAcknowledgeFailure,
	}
	ackCmd.Flags().String("instance", "", "Required: the collector instance id")
	ackCmd.Flags().String("scope", "", "Required: the failed scope (last_failure_scope_id)")
	ackCmd.Flags().String("failure-class", "", "Required: the last_failure_class you read; refused if the failure changed class")
	ackCmd.Flags().String("reason", "", "Required: why the failure is known and expected (stored and shown on status; no secrets)")
	addRemoteFlags(ackCmd)
	return ackCmd
}

func runAdminCollectorAcknowledgeFailure(cmd *cobra.Command, args []string) error {
	instance, _ := cmd.Flags().GetString("instance")
	scope, _ := cmd.Flags().GetString("scope")
	failureClass, _ := cmd.Flags().GetString("failure-class")
	reason, _ := cmd.Flags().GetString("reason")
	result, err := admin.AcknowledgeTerminalFailure(apiClientFromCmd(cmd), admin.AcknowledgeTerminalFailureInput{
		InstanceID: instance, ScopeID: scope, FailureClass: failureClass, Reason: reason,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}
