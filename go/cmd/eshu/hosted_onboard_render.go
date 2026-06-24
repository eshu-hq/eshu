// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// renderHostedOnboardJSON serializes the onboarding artifact as indented JSON.
// The artifact is already redacted, so the bytes are safe to persist or hand to
// a project team.
func renderHostedOnboardJSON(artifact hostedOnboardArtifact) ([]byte, error) {
	return json.MarshalIndent(artifact, "", "  ")
}

// renderHostedOnboardMarkdown renders the artifact as a compact Markdown packet
// suitable for handing to a project team. It reads only already-redacted fields,
// so no endpoint credential or token value can leak through this surface.
func renderHostedOnboardMarkdown(artifact hostedOnboardArtifact) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Hosted onboarding: %s\n\n", artifact.Team)

	fmt.Fprintf(&b, "## Connection\n\n")
	fmt.Fprintf(&b, "- API URL: `%s`\n", onboardValue(artifact.APIURL))
	fmt.Fprintf(&b, "- MCP URL: `%s`\n", onboardValue(artifact.MCPURL))
	fmt.Fprintf(&b, "- Token source: `%s` (configure this; the value is never shown)\n", onboardValue(artifact.TokenSourceName))
	if strings.TrimSpace(artifact.SetupSnippet) != "" {
		fmt.Fprintf(&b, "\n```\n%s\n```\n", strings.TrimRight(artifact.SetupSnippet, "\n"))
	}

	fmt.Fprintf(&b, "\n## Repository scope\n\n")
	fmt.Fprintf(&b, "- Broad: `%t`\n", artifact.RuleScope.Broad)
	if artifact.RuleScope.Broad {
		fmt.Fprintf(&b, "- Broad confirmed: `%t`\n", artifact.RuleScope.Confirmed)
		if reason := strings.TrimSpace(artifact.RuleScope.Reason); reason != "" {
			fmt.Fprintf(&b, "- Reason: %s\n", reason)
		}
	}
	if len(artifact.RuleScope.Rules) > 0 {
		fmt.Fprintf(&b, "- Rules: %s\n", onboardCodeList(artifact.RuleScope.Rules))
	}

	fmt.Fprintf(&b, "\n## Indexing\n\n")
	fmt.Fprintf(&b, "- Index state: `%s`\n", onboardValue(artifact.IndexState))
	fmt.Fprintf(&b, "- Queue/completeness: %s\n", onboardValue(artifact.QueueStatus))
	if len(artifact.IndexedRepositories) > 0 {
		fmt.Fprintf(&b, "- Indexed repositories: %s\n", strings.Join(artifact.IndexedRepositories, "; "))
	}
	fmt.Fprintf(&b, "- First query answered: `%t`\n", artifact.Connection.QueryAnswered)
	if summary := strings.TrimSpace(artifact.Connection.QuerySummary); summary != "" {
		fmt.Fprintf(&b, "- First answer: %s\n", summary)
	}
	fmt.Fprintf(&b, "- MCP tools visible: `%d`\n", artifact.Connection.ToolCount)

	onboardMarkdownList(&b, "Starter prompts", artifact.StarterPrompts)
	onboardMarkdownStarterPlaybooks(&b, artifact.StarterPlaybooks)
	onboardMarkdownList(&b, "Next steps", artifact.NextSteps)

	fmt.Fprintf(&b, "\n## Authorization limitation\n\n")
	fmt.Fprintf(&b, "%s\n", artifact.ScopedIsolationLimitation)
	return b.String(), nil
}

// renderHostedOnboardTerminal writes a concise operator-facing summary of the
// artifact. Like the artifact renderers it reads only redacted fields.
func renderHostedOnboardTerminal(w io.Writer, artifact hostedOnboardArtifact, runErr error) {
	header := "Hosted onboarding ready"
	if !artifact.Connection.QueryAnswered {
		header = "Hosted onboarding incomplete"
	}
	_, _ = fmt.Fprintln(w, header)
	_, _ = fmt.Fprintln(w, strings.Repeat("-", 40))
	_, _ = fmt.Fprintf(w, "  team          : %s\n", artifact.Team)
	_, _ = fmt.Fprintf(w, "  api url       : %s\n", onboardValue(artifact.APIURL))
	_, _ = fmt.Fprintf(w, "  mcp url       : %s\n", onboardValue(artifact.MCPURL))
	_, _ = fmt.Fprintf(w, "  token source  : %s\n", onboardValue(artifact.TokenSourceName))
	_, _ = fmt.Fprintf(w, "  rule scope    : %s\n", onboardRuleScopeLine(artifact.RuleScope))
	_, _ = fmt.Fprintf(w, "  index state   : %s\n", onboardValue(artifact.IndexState))
	_, _ = fmt.Fprintf(w, "  queue status  : %s\n", onboardValue(artifact.QueueStatus))
	_, _ = fmt.Fprintf(w, "  first query   : %t\n", artifact.Connection.QueryAnswered)

	for _, stage := range artifact.Connection.Stages {
		marker := hostedStageMarker(stage.Status)
		line := fmt.Sprintf("  %s %s", marker, stage.Name)
		if stage.Category != hostedFailNone {
			line += fmt.Sprintf(" [%s]", stage.Category)
		}
		if stage.Detail != "" {
			line += ": " + stage.Detail
		}
		_, _ = fmt.Fprintln(w, line)
	}

	if runErr != nil {
		_, _ = fmt.Fprintf(w, "  cause         : %s\n", runErr.Error())
	}
	onboardTerminalList(w, "starter prompts", artifact.StarterPrompts)
	onboardTerminalStarterPlaybooks(w, artifact.StarterPlaybooks)
	onboardTerminalList(w, "next steps", artifact.NextSteps)
	_, _ = fmt.Fprintf(w, "limitation: %s\n", artifact.ScopedIsolationLimitation)
}

// onboardRuleScopeLine renders the rule scope as a one-line terminal summary.
func onboardRuleScopeLine(scope hostedOnboardRuleScope) string {
	if !scope.Broad {
		return "narrow"
	}
	if scope.Confirmed {
		return "broad (confirmed)"
	}
	return "broad (rejected)"
}

// onboardValue substitutes a stable placeholder for an empty value so a code
// span or terminal field never renders blank.
func onboardValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

// onboardCodeList renders a slice as a comma-separated list of inline code spans.
func onboardCodeList(values []string) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, "`"+v+"`")
	}
	return strings.Join(parts, ", ")
}

// onboardMarkdownList writes a titled bullet section when the slice is non-empty.
func onboardMarkdownList(b *strings.Builder, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	for _, v := range values {
		fmt.Fprintf(b, "- %s\n", v)
	}
}

func onboardMarkdownStarterPlaybooks(b *strings.Builder, playbooks []hostedOnboardStarterPlaybook) {
	if len(playbooks) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## Starter playbooks\n\n")
	for _, playbook := range playbooks {
		fmt.Fprintf(
			b,
			"- `%s@%s` (`%s`) tools `%s`; expected truth `%s`; prompt: %s\n",
			playbook.PlaybookID,
			playbook.Version,
			playbook.PromptFamily,
			strings.Join(playbook.Tools, " -> "),
			strings.Join(playbook.ExpectedTruthClasses, ", "),
			playbook.Prompt,
		)
	}
}

// onboardTerminalList writes a titled, indented bullet section to the terminal
// when the slice is non-empty.
func onboardTerminalList(w io.Writer, title string, values []string) {
	if len(values) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "%s:\n", title)
	for _, v := range values {
		_, _ = fmt.Fprintf(w, "  - %s\n", v)
	}
}

func onboardTerminalStarterPlaybooks(w io.Writer, playbooks []hostedOnboardStarterPlaybook) {
	if len(playbooks) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, "starter playbooks:")
	for _, playbook := range playbooks {
		_, _ = fmt.Fprintf(
			w,
			"  - %s@%s (%s): %s; truth=%s\n",
			playbook.PlaybookID,
			playbook.Version,
			playbook.PromptFamily,
			strings.Join(playbook.Tools, " -> "),
			strings.Join(playbook.ExpectedTruthClasses, ","),
		)
	}
}
