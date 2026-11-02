// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// notAvailable marks a headroom reading the source cannot produce. It is
// reported as NOT_AVAILABLE, never as zero.
const notAvailable = -1

// HeadroomSample is one pg_headroom reading. A field at notAvailable was not
// measurable from this source.
type HeadroomSample struct {
	Connections       int     `json:"connections"`
	LockWaiters       int     `json:"lock_waiters"`
	MaxLockWaitMS     float64 `json:"max_lock_wait_ms"`
	DeadlocksTotal    float64 `json:"deadlocks_total"`
	ReplicationLagSec float64 `json:"replication_lag_seconds"`
}

// HeadroomSource reads the database server's headroom.
type HeadroomSource interface {
	Name() string
	Sample(ctx context.Context) (HeadroomSample, error)
}

// SQLHeadroom reads headroom straight from a local server's statistics
// views. Container CPU and memory are not visible from SQL and stay
// NOT_AVAILABLE on this source.
type SQLHeadroom struct {
	DB *sql.DB
}

// Name implements HeadroomSource.
func (h SQLHeadroom) Name() string {
	return "sql:pg_stat_activity,pg_locks,pg_stat_database,pg_stat_replication"
}

// Sample implements HeadroomSource. The lock wait is the longest current
// wait from pg_locks.waitstart (Postgres 14+), the exact §6 quantity.
func (h SQLHeadroom) Sample(ctx context.Context) (HeadroomSample, error) {
	var s HeadroomSample
	var lag sql.NullFloat64
	err := h.DB.QueryRowContext(ctx, `SELECT
    (SELECT count(*) FROM pg_stat_activity),
    (SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock'),
    (SELECT COALESCE(EXTRACT(EPOCH FROM max(now() - waitstart)) * 1000, 0)::float8
        FROM pg_locks WHERE NOT granted AND waitstart IS NOT NULL),
    (SELECT COALESCE(sum(deadlocks), 0)::float8 FROM pg_stat_database),
    (SELECT max(EXTRACT(EPOCH FROM replay_lag))::float8 FROM pg_stat_replication)`).Scan(
		&s.Connections, &s.LockWaiters, &s.MaxLockWaitMS, &s.DeadlocksTotal, &lag)
	if err != nil {
		return s, fmt.Errorf("sql headroom: %w", err)
	}
	s.ReplicationLagSec = notAvailable
	if lag.Valid {
		s.ReplicationLagSec = lag.Float64
	}
	return s, nil
}

// ExporterHeadroom reads headroom from a postgres-exporter /metrics page
// (the live leg reaches it through a port-forward). The exporter has no
// lock-wait duration, so MaxLockWaitMS is NOT_AVAILABLE and the lock-waiter
// count is the bound the live leg enforces.
type ExporterHeadroom struct {
	URL    string
	Client *http.Client
	// LockDatabase, when set, counts lock waiters only on that database
	// (the live ceiling protects core's database, DP-A §2).
	LockDatabase string
}

// Name implements HeadroomSource.
func (h ExporterHeadroom) Name() string { return "postgres-exporter:" + h.URL }

// Sample implements HeadroomSource.
func (h ExporterHeadroom) Sample(ctx context.Context) (HeadroomSample, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return HeadroomSample{}, fmt.Errorf("exporter request: %w", err)
	}
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return HeadroomSample{}, fmt.Errorf("exporter scrape: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return HeadroomSample{}, fmt.Errorf("exporter scrape: status %d", resp.StatusCode)
	}
	return parseExporter(resp.Body, h.LockDatabase)
}

// parseExporter folds the exporter series the ceilings use. A series family
// absent from the page leaves its field NOT_AVAILABLE.
func parseExporter(r io.Reader, lockDatabase string) (HeadroomSample, error) {
	s := HeadroomSample{
		Connections: notAvailable, LockWaiters: notAvailable,
		MaxLockWaitMS: notAvailable, DeadlocksTotal: notAvailable, ReplicationLagSec: notAvailable,
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, labels, value, err := parseSeries(line)
		if err != nil {
			return s, err
		}
		switch name {
		case "pg_stat_database_numbackends":
			s.Connections = addInt(s.Connections, int(value))
		case "pg_stat_activity_count":
			if lockDatabase != "" && labels["datname"] != lockDatabase {
				continue
			}
			if labels["wait_event_type"] == "Lock" {
				s.LockWaiters = addInt(s.LockWaiters, int(value))
			} else if _, ok := labels["wait_event_type"]; ok && s.LockWaiters == notAvailable {
				s.LockWaiters = 0
			}
		case "pg_stat_database_deadlocks":
			s.DeadlocksTotal = addFloat(s.DeadlocksTotal, value)
		case "pg_replication_lag_seconds", "pg_replication_lag":
			if s.ReplicationLagSec == notAvailable || value > s.ReplicationLagSec {
				s.ReplicationLagSec = value
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return s, fmt.Errorf("read exporter page: %w", err)
	}
	return s, nil
}

func addInt(acc, v int) int {
	if acc == notAvailable {
		return v
	}
	return acc + v
}

func addFloat(acc, v float64) float64 {
	if acc == notAvailable {
		return v
	}
	return acc + v
}

// parseSeries parses one Prometheus text-format sample line.
func parseSeries(line string) (string, map[string]string, float64, error) {
	labels := map[string]string{}
	name := line
	rest := ""
	if i := strings.IndexAny(line, "{ "); i >= 0 {
		name, rest = line[:i], line[i:]
	}
	if strings.HasPrefix(rest, "{") {
		end, err := parseLabels(rest, labels)
		if err != nil {
			return "", nil, 0, fmt.Errorf("exporter line %q: %w", line, err)
		}
		rest = rest[end:]
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", nil, 0, fmt.Errorf("exporter line %q: no value", line)
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "", nil, 0, fmt.Errorf("exporter line %q: %w", line, err)
	}
	return name, labels, value, nil
}

// parseLabels reads `{k="v",...}` from s into labels and returns the index
// after the closing brace. Escaped quotes inside values are honoured.
func parseLabels(s string, labels map[string]string) (int, error) {
	i := 1
	for i < len(s) {
		if s[i] == '}' {
			return i + 1, nil
		}
		eq := strings.IndexByte(s[i:], '=')
		if eq < 0 || i+eq+1 >= len(s) || s[i+eq+1] != '"' {
			return 0, fmt.Errorf("malformed labels")
		}
		key := strings.TrimSpace(strings.TrimLeft(s[i:i+eq], ","))
		j := i + eq + 2
		var b strings.Builder
		for j < len(s) && s[j] != '"' {
			if s[j] == '\\' && j+1 < len(s) {
				j++
			}
			b.WriteByte(s[j])
			j++
		}
		if j >= len(s) {
			return 0, fmt.Errorf("unterminated label value")
		}
		labels[key] = b.String()
		i = j + 1
		if i < len(s) && s[i] == ',' {
			i++
		}
	}
	return 0, fmt.Errorf("unterminated labels")
}

// headroomCheck compares one sample against the ceilings and the run's
// deadlock and lock-wait baselines; it returns the first breach.
func headroomCheck(s HeadroomSample, limits Headroom, lockWaitBound time.Duration, deadlockBase float64) *Violation {
	switch {
	case s.DeadlocksTotal != notAvailable && deadlockBase != notAvailable && s.DeadlocksTotal > deadlockBase:
		return violation(TriggerDeadlock, "deadlock counter rose from %.0f to %.0f", deadlockBase, s.DeadlocksTotal)
	case s.MaxLockWaitMS != notAvailable && s.MaxLockWaitMS > float64(lockWaitBound.Milliseconds()):
		return violation(TriggerLockWait, "lock wait %.0f ms above bound %d ms", s.MaxLockWaitMS, lockWaitBound.Milliseconds())
	case s.LockWaiters != notAvailable && s.LockWaiters > limits.MaxLockWaiters:
		return violation(TriggerHeadroom, "%d sessions waiting on locks, ceiling %d", s.LockWaiters, limits.MaxLockWaiters)
	case s.Connections != notAvailable && s.Connections > limits.MaxConnections:
		return violation(TriggerHeadroom, "%d connections, ceiling %d", s.Connections, limits.MaxConnections)
	case s.ReplicationLagSec != notAvailable && limits.MaxLagSeconds > 0 && s.ReplicationLagSec > limits.MaxLagSeconds:
		return violation(TriggerHeadroom, "replication lag %.1fs, ceiling %.1fs", s.ReplicationLagSec, limits.MaxLagSeconds)
	}
	return nil
}
