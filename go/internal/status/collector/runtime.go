// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/cloud"
)

const (
	// RuntimeCoordinatorManaged identifies a collector registered with
	// the workflow coordinator and eligible for claim-driven work.
	RuntimeCoordinatorManaged = "coordinator_managed"
	// RuntimeDirectMode identifies a configured collector whose
	// coordinator row is enabled but not claim-driven.
	RuntimeDirectMode = "direct_mode"
	// RuntimeProfileGated identifies a collector intentionally hidden
	// by an active runtime profile gate.
	RuntimeProfileGated = "profile_gated"
	// RuntimeDisabled identifies a registered collector disabled by
	// configuration or deactivated by reconciliation.
	RuntimeDisabled = "disabled"
	// RuntimeUnregistered identifies runtime status evidence without a
	// matching workflow coordinator registration.
	RuntimeUnregistered = "unregistered"
)

// RuntimeStatus is the unified operator view of one collector runtime
// identity across coordinator registration and direct status evidence.
type RuntimeStatus struct {
	InstanceID            string
	CollectorKind         string
	Mode                  string
	RuntimeMode           string
	StatusCategory        string
	CoordinatorRegistered bool
	Enabled               bool
	Bootstrap             bool
	ClaimsEnabled         bool
	DisplayName           string
	Health                string
	EvidenceSources       []string
	SourceSystems         []string
	ObservationCount      int
	LastObservedAt        time.Time
	UpdatedAt             time.Time
	DeactivatedAt         time.Time
	Detail                string
}

// RuntimeStatuses derives the shared collector runtime readback from
// the collector evidence in the status report, without performing I/O.
func RuntimeStatuses(evidence Evidence) []RuntimeStatus {
	builder := runtimeStatusBuilder{byKey: map[string]int{}}
	for _, instance := range evidence.Instances {
		builder.add(coordinatorRuntimeStatus(instance))
	}
	builder.addAWSScans(evidence.AWSScans)
	builder.addVulnerabilitySources(evidence.VulnerabilitySources)
	builder.addFactEvidence(evidence.FactEvidence)
	return builder.rows()
}

type runtimeStatusBuilder struct {
	statuses []RuntimeStatus
	byKey    map[string]int
}

func (b *runtimeStatusBuilder) add(status RuntimeStatus) int {
	status.InstanceID = strings.TrimSpace(status.InstanceID)
	status.CollectorKind = strings.TrimSpace(status.CollectorKind)
	if status.InstanceID == "" || status.CollectorKind == "" {
		return -1
	}
	key := runtimeStatusKey(status.CollectorKind, status.InstanceID)
	if index, ok := b.byKey[key]; ok {
		b.merge(index, status)
		return index
	}
	status.EvidenceSources = uniqueNonEmptyStrings(status.EvidenceSources)
	status.SourceSystems = uniqueNonEmptyStrings(status.SourceSystems)
	b.byKey[key] = len(b.statuses)
	b.statuses = append(b.statuses, status)
	return len(b.statuses) - 1
}

func (b *runtimeStatusBuilder) merge(index int, status RuntimeStatus) {
	existing := &b.statuses[index]
	existing.EvidenceSources = uniqueNonEmptyStrings(append(existing.EvidenceSources, status.EvidenceSources...))
	existing.SourceSystems = uniqueNonEmptyStrings(append(existing.SourceSystems, status.SourceSystems...))
	existing.ObservationCount += status.ObservationCount
	if runtimeHealthRank(status.Health) > runtimeHealthRank(existing.Health) && strings.TrimSpace(status.Detail) != "" {
		existing.Detail = status.Detail
	}
	existing.Health = combineRuntimeHealth(existing.Health, status.Health)
	if status.LastObservedAt.After(existing.LastObservedAt) {
		existing.LastObservedAt = status.LastObservedAt
	}
	if status.UpdatedAt.After(existing.UpdatedAt) {
		existing.UpdatedAt = status.UpdatedAt
	}
}

func (b *runtimeStatusBuilder) addAWSScans(rows []cloud.AWSScanStatus) {
	type aggregate struct {
		count          int
		health         string
		detail         string
		lastObservedAt time.Time
		updatedAt      time.Time
	}
	aggregates := map[string]aggregate{}
	for _, row := range rows {
		instanceID := strings.TrimSpace(row.CollectorInstanceID)
		if instanceID == "" {
			continue
		}
		current := aggregates[instanceID]
		current.count++
		health := awsCloudScanHealth(row)
		if health != "observed" && health != "unknown" && runtimeHealthRank(health) > runtimeHealthRank(current.health) {
			current.detail = awsCloudScanDetail(row)
		}
		current.health = combineRuntimeHealth(current.health, health)
		if row.LastObservedAt.After(current.lastObservedAt) {
			current.lastObservedAt = row.LastObservedAt
		}
		if row.UpdatedAt.After(current.updatedAt) {
			current.updatedAt = row.UpdatedAt
		}
		aggregates[instanceID] = current
	}
	for instanceID, aggregate := range aggregates {
		status := directEvidenceRuntimeStatus(
			instanceID,
			"aws",
			"aws_cloud_scan_status",
			nil,
			aggregate.count,
			aggregate.health,
			aggregate.lastObservedAt,
			aggregate.updatedAt,
		)
		if aggregate.detail != "" {
			status.Detail = aggregate.detail
		}
		b.add(status)
	}
}

func (b *runtimeStatusBuilder) addVulnerabilitySources(rows []VulnerabilitySourceState) {
	type aggregate struct {
		count     int
		health    string
		updatedAt time.Time
	}
	aggregates := map[string]aggregate{}
	for _, row := range rows {
		instanceID := strings.TrimSpace(row.CollectorInstanceID)
		if instanceID == "" {
			continue
		}
		current := aggregates[instanceID]
		current.count++
		current.health = combineRuntimeHealth(current.health, vulnerabilitySourceHealth(row))
		if row.UpdatedAt.After(current.updatedAt) {
			current.updatedAt = row.UpdatedAt
		}
		aggregates[instanceID] = current
	}
	for instanceID, aggregate := range aggregates {
		b.add(directEvidenceRuntimeStatus(
			instanceID,
			"vulnerability_intelligence",
			"vulnerability_source_state",
			nil,
			aggregate.count,
			aggregate.health,
			time.Time{},
			aggregate.updatedAt,
		))
	}
}

func (b *runtimeStatusBuilder) addFactEvidence(rows []FactEvidence) {
	for _, row := range rows {
		instanceID := strings.TrimSpace(row.InstanceID)
		collectorKind := strings.TrimSpace(row.CollectorKind)
		if instanceID == "" {
			instanceID = b.instanceIDForCollectorKind(collectorKind)
		}
		b.add(directEvidenceRuntimeStatus(
			instanceID,
			collectorKind,
			row.EvidenceSource,
			row.SourceSystems,
			row.ObservationCount,
			"observed",
			row.LastObservedAt,
			row.UpdatedAt,
		))
	}
}

func (b runtimeStatusBuilder) instanceIDForCollectorKind(collectorKind string) string {
	if collectorKind == "" {
		return ""
	}
	coordinatorMatches := []string{}
	for _, status := range b.statuses {
		if status.CollectorKind != collectorKind || !status.CoordinatorRegistered {
			continue
		}
		coordinatorMatches = append(coordinatorMatches, status.InstanceID)
	}
	if len(coordinatorMatches) == 1 {
		return coordinatorMatches[0]
	}
	matches := []string{}
	for _, status := range b.statuses {
		if status.CollectorKind != collectorKind {
			continue
		}
		matches = append(matches, status.InstanceID)
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return collectorKind + "-persisted-facts"
}

func (b runtimeStatusBuilder) rows() []RuntimeStatus {
	statuses := slices.Clone(b.statuses)
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].CollectorKind != statuses[j].CollectorKind {
			return statuses[i].CollectorKind < statuses[j].CollectorKind
		}
		return statuses[i].InstanceID < statuses[j].InstanceID
	})
	return statuses
}

func coordinatorRuntimeStatus(instance InstanceSummary) RuntimeStatus {
	category := RuntimeCoordinatorManaged
	mode := CollectorClaimDriven
	detail := "registered with workflow coordinator"
	health := "registered"
	if !instance.Enabled || !instance.DeactivatedAt.IsZero() {
		category = RuntimeDisabled
		mode = CollectorClaimRegistration
		health = "disabled"
		detail = "registered but disabled or deactivated"
	} else if !instance.ClaimsEnabled {
		category = RuntimeDirectMode
		mode = CollectorClaimDirect
		detail = "registered with claims disabled; direct-mode or profile-gated runtime"
	}
	return RuntimeStatus{
		InstanceID:            instance.InstanceID,
		CollectorKind:         instance.CollectorKind,
		Mode:                  instance.Mode,
		RuntimeMode:           mode,
		StatusCategory:        category,
		CoordinatorRegistered: true,
		Enabled:               instance.Enabled,
		Bootstrap:             instance.Bootstrap,
		ClaimsEnabled:         instance.ClaimsEnabled,
		DisplayName:           instance.DisplayName,
		Health:                health,
		EvidenceSources:       []string{"workflow_coordinator"},
		LastObservedAt:        instance.LastObservedAt,
		UpdatedAt:             instance.UpdatedAt,
		DeactivatedAt:         instance.DeactivatedAt,
		Detail:                detail,
	}
}

func directEvidenceRuntimeStatus(
	instanceID string,
	collectorKind string,
	evidenceSource string,
	sourceSystems []string,
	observations int,
	health string,
	lastObservedAt time.Time,
	updatedAt time.Time,
) RuntimeStatus {
	return RuntimeStatus{
		InstanceID:       instanceID,
		CollectorKind:    collectorKind,
		RuntimeMode:      CollectorClaimDirect,
		StatusCategory:   RuntimeUnregistered,
		Health:           health,
		EvidenceSources:  []string{evidenceSource},
		SourceSystems:    uniqueNonEmptyStrings(sourceSystems),
		ObservationCount: observations,
		LastObservedAt:   lastObservedAt,
		UpdatedAt:        updatedAt,
		Detail:           "status evidence exists without workflow coordinator registration",
	}
}

func awsCloudScanHealth(row cloud.AWSScanStatus) string {
	status := strings.TrimSpace(row.Status)
	if awsCloudScanSucceeded(status) && strings.TrimSpace(row.CommitStatus) == "committed" {
		return "observed"
	}
	if row.CredentialFailed || row.BudgetExhausted || strings.TrimSpace(row.FailureClass) != "" {
		return "degraded"
	}
	switch status {
	case "failed", "failed_terminal", "failed_retryable":
		return "degraded"
	case "partial":
		return "partial"
	case "succeeded", "success", "completed":
		return "observed"
	default:
		return "unknown"
	}
}

func awsCloudScanSucceeded(status string) bool {
	switch status {
	case "succeeded", "success", "completed":
		return true
	default:
		return false
	}
}

func awsCloudScanDetail(row cloud.AWSScanStatus) string {
	parts := []string{"aws_cloud_scan_status"}
	if status := strings.TrimSpace(row.Status); status != "" {
		parts = append(parts, "status="+status)
	}
	if commitStatus := strings.TrimSpace(row.CommitStatus); commitStatus != "" {
		parts = append(parts, "commit_status="+commitStatus)
	}
	if failureClass := strings.TrimSpace(row.FailureClass); failureClass != "" {
		parts = append(parts, "failure_class="+failureClass)
	}
	if row.BudgetExhausted {
		parts = append(parts, "budget_exhausted=true")
	}
	if row.CredentialFailed {
		parts = append(parts, "credential_failed=true")
	}
	if service := strings.TrimSpace(row.ServiceKind); service != "" {
		parts = append(parts, "service="+service)
	}
	if region := strings.TrimSpace(row.Region); region != "" {
		parts = append(parts, "region="+region)
	}
	return strings.Join(parts, " ")
}

func vulnerabilitySourceHealth(row VulnerabilitySourceState) string {
	switch strings.TrimSpace(row.TerminalStatus) {
	case "failed", "failed_terminal", "failed_retryable":
		return "degraded"
	case "partial":
		return "partial"
	case "succeeded", "completed":
		return "observed"
	default:
		return "unknown"
	}
}

func combineRuntimeHealth(current string, next string) string {
	if runtimeHealthRank(next) > runtimeHealthRank(current) {
		return next
	}
	return current
}

func runtimeHealthRank(value string) int {
	switch value {
	case "degraded":
		return 4
	case "partial":
		return 3
	case "disabled", "observed":
		return 2
	case "unknown", "registered":
		return 1
	default:
		return 0
	}
}

func runtimeStatusKey(collectorKind string, instanceID string) string {
	return collectorKind + "\x00" + instanceID
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	output := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		output = append(output, value)
	}
	sort.Strings(output)
	return output
}

func RenderRuntimeStatusLines(rows []RuntimeStatus) []string {
	if len(rows) == 0 {
		return nil
	}
	lines := []string{"Collector runtimes:"}
	for _, row := range rows {
		line := fmt.Sprintf(
			"  %s kind=%s category=%s mode=%s coordinator_registered=%t evidence=%s",
			row.InstanceID,
			row.CollectorKind,
			row.StatusCategory,
			row.RuntimeMode,
			row.CoordinatorRegistered,
			strings.Join(row.EvidenceSources, ","),
		)
		if len(row.SourceSystems) > 0 {
			line += fmt.Sprintf(" source_systems=%s", strings.Join(row.SourceSystems, ","))
		}
		if row.Health != "" {
			line += fmt.Sprintf(" health=%s", row.Health)
		}
		if row.ObservationCount > 0 {
			line += fmt.Sprintf(" observations=%d", row.ObservationCount)
		}
		lines = append(lines, line)
	}
	return lines
}
