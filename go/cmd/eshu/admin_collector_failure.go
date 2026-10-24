// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/admin"
)

var adminCollectorFailureCmd = &cobra.Command{
	Use:   "collector-failure",
	Short: "Collector terminal failure administration",
}

func init() {
	adminCmd.AddCommand(adminCollectorFailureCmd)
	adminCollectorFailureCmd.AddCommand(newAdminCollectorFailureAcknowledgeCmd())
}

// newAdminCollectorFailureAcknowledgeCmd builds
// `eshu admin collector-failure acknowledge` (#8830).
func newAdminCollectorFailureAcknowledgeCmd() *cobra.Command {
	ackCmd := &cobra.Command{
		Use:   "acknowledge",
		Short: "Acknowledge a collector terminal failure as known and expected",
		Long: "Marks the newest unresolved terminal failure of one collector instance and scope as known and expected. " +
			"The instance then reads partial instead of failed, with the failure still named, until the failure changes " +
			"class or fails again after a completed work item. " +
			"Read --collector-kind and --instance from GET /api/v0/status/collector-readiness or the " +
			"get_collector_readiness MCP tool, and find --scope with POST /api/v0/admin/work-items/query " +
			"(statuses failed_terminal). The reason is stored and shown to admins; do not put secrets in it.",
		RunE: runAdminCollectorFailureAcknowledge,
	}
	ackCmd.Flags().String("collector-kind", "", "Required: the collector kind of the instance")
	ackCmd.Flags().String("instance", "", "Required: the collector instance id")
	ackCmd.Flags().String("scope", "", "Required: the failed scope id")
	ackCmd.Flags().String("failure-class", "", "Optional: the last_failure_class you read; refused if the failure changed class")
	ackCmd.Flags().String("reason", "", "Required: why the failure is known and expected")
	ackCmd.Flags().String("idempotency-key", "", "Idempotency key; one is generated and printed when empty")
	addRemoteFlags(ackCmd)
	return ackCmd
}

func runAdminCollectorFailureAcknowledge(cmd *cobra.Command, args []string) error {
	kind, _ := cmd.Flags().GetString("collector-kind")
	instance, _ := cmd.Flags().GetString("instance")
	scope, _ := cmd.Flags().GetString("scope")
	failureClass, _ := cmd.Flags().GetString("failure-class")
	reason, _ := cmd.Flags().GetString("reason")
	key, _ := cmd.Flags().GetString("idempotency-key")
	if key == "" {
		generated, err := admin.NewAcknowledgeFailureIdempotencyKey()
		if err != nil {
			return err
		}
		key = generated
	}
	// Print the key before the call, so an operator can retry with it even when
	// the call fails after the acknowledgement committed.
	fmt.Fprintf(os.Stderr, "idempotency key: %s\n", key)
	result, err := admin.AcknowledgeCollectorFailure(apiClientFromCmd(cmd), admin.AcknowledgeCollectorFailureInput{
		CollectorKind: kind, InstanceID: instance, ScopeID: scope, FailureClass: failureClass,
		Reason: reason, IdempotencyKey: key,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}
