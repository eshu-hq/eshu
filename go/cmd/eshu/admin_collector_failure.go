// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
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
	adminCollectorFailureCmd.AddCommand(newAdminCollectorFailureListCmd())
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
			"get_collector_readiness MCP tool, and find --scope with `eshu admin collector-failure list`. " +
			"The reason is stored and shown to admins; do not put secrets in it.",
		RunE: runAdminCollectorFailureAcknowledge,
	}
	ackCmd.Flags().String("collector-kind", "", "Required: the collector kind of the instance")
	ackCmd.Flags().String("instance", "", "Required: the collector instance id")
	ackCmd.Flags().String("scope", "", "Required: the failed scope id")
	ackCmd.Flags().String("reason", "", "Required: why the failure is known and expected")
	ackCmd.Flags().String("idempotency-key", "", "Idempotency key; one is generated and printed when empty")
	addRemoteFlags(ackCmd)
	return ackCmd
}

func runAdminCollectorFailureAcknowledge(cmd *cobra.Command, args []string) error {
	kind, _ := cmd.Flags().GetString("collector-kind")
	instance, _ := cmd.Flags().GetString("instance")
	scope, _ := cmd.Flags().GetString("scope")
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
		CollectorKind: kind, InstanceID: instance, ScopeID: scope,
		Reason: reason, IdempotencyKey: key,
	})
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

// newAdminCollectorFailureListCmd builds `eshu admin collector-failure list`
// (#8830), the discovery step before `acknowledge`.
func newAdminCollectorFailureListCmd() *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List the failed scopes of one collector instance",
		Long: "Lists the unresolved terminal failures of one collector instance, per scope, with the scope ids " +
			"`eshu admin collector-failure acknowledge` accepts. It shows exactly what collector status counts: " +
			"inside the instance's window, not resolved by a completed work item, and whether an operator " +
			"acknowledged each. Needs an admin token. Status surfaces never carry scope ids.",
		RunE: runAdminCollectorFailureList,
	}
	listCmd.Flags().String("collector-kind", "", "Required: the collector kind of the instance")
	listCmd.Flags().String("instance", "", "Required: the collector instance id")
	listCmd.Flags().Int("limit", 0, "Scopes per page (default 100, at most 500)")
	listCmd.Flags().String("cursor", "", "The next cursor printed by the previous page")
	addRemoteFlags(listCmd)
	return listCmd
}

func runAdminCollectorFailureList(cmd *cobra.Command, args []string) error {
	kind, _ := cmd.Flags().GetString("collector-kind")
	instance, _ := cmd.Flags().GetString("instance")
	limit, _ := cmd.Flags().GetInt("limit")
	cursor, _ := cmd.Flags().GetString("cursor")
	page, err := admin.ListCollectorFailures(apiClientFromCmd(cmd), admin.ListCollectorFailuresInput{
		CollectorKind: kind, InstanceID: instance, Limit: limit, Cursor: cursor,
	})
	if err != nil {
		return err
	}
	writeCollectorFailureList(cmd.OutOrStdout(), cmd.ErrOrStderr(), page)
	return nil
}

// writeCollectorFailureList prints one line per item (scope_id failure_class
// failure_operation failed_at acknowledged work_item_id) and, when the page is
// truncated, the cursor for the next page on the error stream.
func writeCollectorFailureList(out, errOut io.Writer, page admin.CollectorFailuresPage) {
	for _, item := range page.Items {
		_, _ = fmt.Fprintf(out, "%s %s %s %s %t %s\n", item.ScopeID, item.FailureClass, item.FailureOperation,
			item.FailedAt, item.Acknowledged, item.WorkItemID)
	}
	if page.Truncated {
		_, _ = fmt.Fprintf(errOut, "next cursor: %s\n", page.NextCursor)
	}
}
