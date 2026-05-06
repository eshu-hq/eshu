package runtime

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const runtimeMetricsContentType = "text/plain; charset=utf-8"

// NewStatusMetricsHandler builds a shared Prometheus-style metrics surface from
// the same status reader used by the runtime admin report.
func NewStatusMetricsHandler(serviceName string, reader statuspkg.Reader) (http.Handler, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return nil, fmt.Errorf("service name is required")
	}
	if reader == nil {
		return nil, fmt.Errorf("status reader is required")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveStatusMetrics(w, r, serviceName, reader)
	}), nil
}

func serveStatusMetrics(w http.ResponseWriter, r *http.Request, serviceName string, reader statuspkg.Reader) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	report, err := statuspkg.LoadReport(r.Context(), reader, time.Now().UTC(), statuspkg.DefaultOptions())
	if err != nil {
		http.Error(w, fmt.Sprintf("load runtime metrics: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", runtimeMetricsContentType)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}

	_, _ = w.Write([]byte(renderStatusMetrics(serviceName, report)))
}

func renderStatusMetrics(serviceName string, report statuspkg.Report) string {
	builder := &strings.Builder{}
	quote := func(value string) string {
		return strconv.Quote(strings.TrimSpace(value))
	}

	writeGauge := func(name string, labels map[string]string, value string) {
		builder.WriteString(name)
		if len(labels) > 0 {
			builder.WriteString("{")
			keys := make([]string, 0, len(labels))
			for key := range labels {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for i, key := range keys {
				if i > 0 {
					builder.WriteString(",")
				}
				builder.WriteString(key)
				builder.WriteString("=")
				builder.WriteString(quote(labels[key]))
			}
			builder.WriteString("}")
		}
		builder.WriteString(" ")
		builder.WriteString(value)
		builder.WriteString("\n")
	}

	baseLabels := map[string]string{
		"service_name":      serviceName,
		"service_namespace": telemetry.DefaultServiceNamespace,
		"version":           buildinfo.AppVersion(),
	}

	writeGauge("eshu_runtime_info", baseLabels, "1")
	writeGauge("eshu_runtime_scope_active", map[string]string{"service_name": serviceName}, strconv.Itoa(report.ScopeActivity.Active))
	writeGauge("eshu_runtime_scope_changed", map[string]string{"service_name": serviceName}, strconv.Itoa(report.ScopeActivity.Changed))
	writeGauge("eshu_runtime_scope_unchanged", map[string]string{"service_name": serviceName}, strconv.Itoa(report.ScopeActivity.Unchanged))
	writeGauge(
		"eshu_runtime_refresh_skipped_total",
		map[string]string{"service_name": serviceName},
		strconv.FormatUint(telemetry.SkippedRefreshCount(), 10),
	)
	for _, row := range report.RetryPolicies {
		labels := map[string]string{
			"service_name": serviceName,
			"stage":        row.Stage,
		}
		writeGauge("eshu_runtime_retry_policy_max_attempts", labels, strconv.Itoa(row.MaxAttempts))
		writeGauge(
			"eshu_runtime_retry_policy_retry_delay_seconds",
			labels,
			fmt.Sprintf("%.0f", row.RetryDelay.Seconds()),
		)
	}
	for _, state := range []string{"healthy", "progressing", "degraded", "stalled"} {
		value := "0"
		if report.Health.State == state {
			value = "1"
		}
		writeGauge("eshu_runtime_health_state", map[string]string{
			"service_name": serviceName,
			"state":        state,
		}, value)
	}

	queue := report.Queue
	writeGauge("eshu_runtime_queue_total", map[string]string{"service_name": serviceName}, strconv.Itoa(queue.Total))
	writeGauge("eshu_runtime_queue_outstanding", map[string]string{"service_name": serviceName}, strconv.Itoa(queue.Outstanding))
	writeGauge("eshu_runtime_queue_pending", map[string]string{"service_name": serviceName}, strconv.Itoa(queue.Pending))
	writeGauge("eshu_runtime_queue_in_flight", map[string]string{"service_name": serviceName}, strconv.Itoa(queue.InFlight))
	writeGauge("eshu_runtime_queue_retrying", map[string]string{"service_name": serviceName}, strconv.Itoa(queue.Retrying))
	writeGauge("eshu_runtime_queue_succeeded", map[string]string{"service_name": serviceName}, strconv.Itoa(queue.Succeeded))
	writeGauge("eshu_runtime_queue_dead_letter", map[string]string{"service_name": serviceName}, strconv.Itoa(queue.DeadLetter))
	writeGauge("eshu_runtime_queue_failed", map[string]string{"service_name": serviceName}, strconv.Itoa(queue.Failed))
	writeGauge("eshu_runtime_queue_overdue_claims", map[string]string{"service_name": serviceName}, strconv.Itoa(queue.OverdueClaims))
	writeGauge(
		"eshu_runtime_queue_oldest_outstanding_age_seconds",
		map[string]string{"service_name": serviceName},
		fmt.Sprintf("%.0f", queue.OldestOutstandingAge.Seconds()),
	)

	for _, key := range sortedCountKeys(report.GenerationTotals) {
		writeGauge(
			"eshu_runtime_generation_total",
			map[string]string{
				"service_name": serviceName,
				"state":        key,
			},
			strconv.Itoa(report.GenerationTotals[key]),
		)
	}

	for _, row := range report.StageSummaries {
		labels := map[string]string{
			"service_name": serviceName,
			"stage":        row.Stage,
		}
		writeGauge("eshu_runtime_stage_items", mergeLabels(labels, "status", "pending"), strconv.Itoa(row.Pending))
		writeGauge("eshu_runtime_stage_items", mergeLabels(labels, "status", "claimed"), strconv.Itoa(row.Claimed))
		writeGauge("eshu_runtime_stage_items", mergeLabels(labels, "status", "running"), strconv.Itoa(row.Running))
		writeGauge("eshu_runtime_stage_items", mergeLabels(labels, "status", "retrying"), strconv.Itoa(row.Retrying))
		writeGauge("eshu_runtime_stage_items", mergeLabels(labels, "status", "succeeded"), strconv.Itoa(row.Succeeded))
		writeGauge("eshu_runtime_stage_items", mergeLabels(labels, "status", "dead_letter"), strconv.Itoa(row.DeadLetter))
		writeGauge("eshu_runtime_stage_items", mergeLabels(labels, "status", "failed"), strconv.Itoa(row.Failed))
	}

	for _, row := range report.DomainBacklogs {
		labels := map[string]string{
			"service_name": serviceName,
			"domain":       row.Domain,
		}
		writeGauge("eshu_runtime_domain_outstanding", labels, strconv.Itoa(row.Outstanding))
		writeGauge("eshu_runtime_domain_retrying", labels, strconv.Itoa(row.Retrying))
		writeGauge("eshu_runtime_domain_dead_letter", labels, strconv.Itoa(row.DeadLetter))
		writeGauge("eshu_runtime_domain_failed", labels, strconv.Itoa(row.Failed))
		writeGauge(
			"eshu_runtime_domain_oldest_age_seconds",
			labels,
			fmt.Sprintf("%.0f", row.OldestAge.Seconds()),
		)
	}
	writeCoordinatorMetrics(writeGauge, serviceName, report.Coordinator)

	return builder.String()
}

func writeCoordinatorMetrics(
	writeGauge func(name string, labels map[string]string, value string),
	serviceName string,
	snapshot *statuspkg.CoordinatorSnapshot,
) {
	if snapshot == nil {
		return
	}

	baseLabels := map[string]string{"service_name": serviceName}
	writeGauge("eshu_runtime_coordinator_active_claims", baseLabels, strconv.Itoa(snapshot.ActiveClaims))
	writeGauge("eshu_runtime_coordinator_overdue_claims", baseLabels, strconv.Itoa(snapshot.OverdueClaims))
	writeGauge(
		"eshu_runtime_coordinator_oldest_pending_age_seconds",
		baseLabels,
		fmt.Sprintf("%.0f", snapshot.OldestPendingAge.Seconds()),
	)
	writeGauge(
		"eshu_runtime_coordinator_collector_instances_total",
		baseLabels,
		strconv.Itoa(len(snapshot.CollectorInstances)),
	)

	for _, row := range summarizeCollectorInstances(snapshot.CollectorInstances) {
		writeGauge(
			"eshu_runtime_coordinator_collector_instances",
			map[string]string{
				"service_name":   serviceName,
				"collector_kind": row.CollectorKind,
				"mode":           row.Mode,
				"enabled":        strconv.FormatBool(row.Enabled),
				"bootstrap":      strconv.FormatBool(row.Bootstrap),
				"claims_enabled": strconv.FormatBool(row.ClaimsEnabled),
				"lifecycle":      row.Lifecycle,
			},
			strconv.Itoa(row.Count),
		)
	}

	for _, row := range snapshot.RunStatusCounts {
		if strings.TrimSpace(row.Name) == "" || row.Count == 0 {
			continue
		}
		writeGauge(
			"eshu_runtime_coordinator_run_status",
			map[string]string{
				"service_name": serviceName,
				"status":       row.Name,
			},
			strconv.Itoa(row.Count),
		)
	}

	for _, row := range snapshot.WorkItemStatusCounts {
		if strings.TrimSpace(row.Name) == "" || row.Count == 0 {
			continue
		}
		writeGauge(
			"eshu_runtime_coordinator_work_item_status",
			map[string]string{
				"service_name": serviceName,
				"status":       row.Name,
			},
			strconv.Itoa(row.Count),
		)
	}

	for _, row := range snapshot.CompletenessCounts {
		if strings.TrimSpace(row.Name) == "" || row.Count == 0 {
			continue
		}
		writeGauge(
			"eshu_runtime_coordinator_completeness",
			map[string]string{
				"service_name": serviceName,
				"state":        row.Name,
			},
			strconv.Itoa(row.Count),
		)
	}
}

type collectorInstanceMetricRow struct {
	CollectorKind string
	Mode          string
	Enabled       bool
	Bootstrap     bool
	ClaimsEnabled bool
	Lifecycle     string
	Count         int
}

func summarizeCollectorInstances(instances []statuspkg.CollectorInstanceSummary) []collectorInstanceMetricRow {
	if len(instances) == 0 {
		return nil
	}

	counts := make(map[string]collectorInstanceMetricRow)
	for _, instance := range instances {
		row := collectorInstanceMetricRow{
			CollectorKind: strings.TrimSpace(instance.CollectorKind),
			Mode:          strings.TrimSpace(instance.Mode),
			Enabled:       instance.Enabled,
			Bootstrap:     instance.Bootstrap,
			ClaimsEnabled: instance.ClaimsEnabled,
			Lifecycle:     "active",
			Count:         1,
		}
		if !instance.DeactivatedAt.IsZero() {
			row.Lifecycle = "deactivated"
		}
		key := strings.Join([]string{
			row.CollectorKind,
			row.Mode,
			strconv.FormatBool(row.Enabled),
			strconv.FormatBool(row.Bootstrap),
			strconv.FormatBool(row.ClaimsEnabled),
			row.Lifecycle,
		}, "\x00")
		if existing, ok := counts[key]; ok {
			existing.Count++
			counts[key] = existing
			continue
		}
		counts[key] = row
	}

	rows := make([]collectorInstanceMetricRow, 0, len(counts))
	for _, row := range counts {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		left := rows[i]
		right := rows[j]
		switch {
		case left.CollectorKind != right.CollectorKind:
			return left.CollectorKind < right.CollectorKind
		case left.Mode != right.Mode:
			return left.Mode < right.Mode
		case left.Enabled != right.Enabled:
			return !left.Enabled && right.Enabled
		case left.Bootstrap != right.Bootstrap:
			return !left.Bootstrap && right.Bootstrap
		case left.ClaimsEnabled != right.ClaimsEnabled:
			return !left.ClaimsEnabled && right.ClaimsEnabled
		case left.Lifecycle != right.Lifecycle:
			return left.Lifecycle < right.Lifecycle
		default:
			return left.Count < right.Count
		}
	})
	return rows
}

func mergeLabels(labels map[string]string, key, value string) map[string]string {
	merged := make(map[string]string, len(labels)+1)
	for labelKey, labelValue := range labels {
		merged[labelKey] = labelValue
	}
	merged[key] = value
	return merged
}

func sortedCountKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if strings.TrimSpace(key) == "" || value == 0 {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
