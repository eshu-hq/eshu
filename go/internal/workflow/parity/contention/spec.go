// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Environment names of the REMOVAL_CONTENTION_SPEC.md §2 fields. Each one is
// required; LoadSpec fails closed and names every blank field.
const (
	EnvTarget             = "ESHU_CONTENTION_TARGET"
	EnvDigest             = "ESHU_CONTENTION_DIGEST_UNDER_TEST"
	EnvIACEnablePR        = "ESHU_CONTENTION_IAC_ENABLE_PR"
	EnvIACRevertPR        = "ESHU_CONTENTION_IAC_REVERT_PR"
	EnvAuthority          = "ESHU_CONTENTION_AUTHORITY_DECISION"
	EnvWorkerCounts       = "ESHU_CONTENTION_WORKER_COUNTS"
	EnvWorkerMultiple     = "ESHU_CONTENTION_WORKER_MULTIPLE"
	EnvBacklogItems       = "ESHU_CONTENTION_BACKLOG_ITEMS"
	EnvBacklogScopes      = "ESHU_CONTENTION_BACKLOG_SCOPES"
	EnvQuotaBudget        = "ESHU_CONTENTION_QUOTA_BUDGET"
	EnvWindowSeconds      = "ESHU_CONTENTION_WINDOW_SECONDS"
	EnvWallClockCap       = "ESHU_CONTENTION_WALL_CLOCK_CAP"
	EnvStarvationBound    = "ESHU_CONTENTION_STARVATION_BOUND_SECONDS"
	EnvLockWaitBoundMS    = "ESHU_CONTENTION_LOCK_WAIT_BOUND_MS"
	EnvPGMaxConnections   = "ESHU_CONTENTION_PG_MAX_CONNECTIONS"
	EnvPGMaxLagSeconds    = "ESHU_CONTENTION_PG_MAX_LAG_SECONDS"
	EnvPGMaxCPUMillicores = "ESHU_CONTENTION_PG_MAX_CPU_MILLICORES"
	EnvPGMaxLockWaiters   = "ESHU_CONTENTION_PG_MAX_LOCK_WAITERS"
	EnvBaselineThroughput = "ESHU_CONTENTION_BASELINE_THROUGHPUT"
	EnvSeed               = "ESHU_CONTENTION_SEED"
	// Optional overrides; the defaults are workflow/defaults.go (60s/20s/20s).
	EnvLeaseTTL          = "ESHU_CONTENTION_LEASE_TTL"
	EnvHeartbeatInterval = "ESHU_CONTENTION_HEARTBEAT_INTERVAL"
	EnvReapInterval      = "ESHU_CONTENTION_REAP_INTERVAL"
	// EnvLiveGrant must name the standing synthetic-write grant comment on a
	// live target; any other value refuses the run.
	EnvLiveGrant = "ESHU_CONTENTION_LIVE_GRANT"
)

// NotApplicableLocal is the explicit sentinel a local-target run records for
// a field that only exists on the deployed target (the IaC PR pair, the
// authority decision, replica lag, container CPU). A live target refuses it.
const NotApplicableLocal = "not_applicable_local"

// NotApplicable records a field that does not apply on this leg (the quota
// budget on a claim plane started without --quotas).
const NotApplicable = "NOT_APPLICABLE"

// LiveGrantComment is the #8875 comment recording the standing synthetic-write
// grant the live leg runs under.
const LiveGrantComment = "6558745112"

// maxWallClockCap is the per seed and leg cap from the DP-A ruling (§2).
const maxWallClockCap = 2 * time.Hour

// Target is where the claim plane under test runs.
type Target string

// Targets: a local disposable container, or the live dark claim plane.
const (
	TargetLocal Target = "local"
	TargetLive  Target = "live"
)

var (
	digestPattern = regexp.MustCompile(`^(ghcr\.io/[a-z0-9./-]+@)?sha256:[0-9a-f]{64}$`)
	prPattern     = regexp.MustCompile(`^#?[0-9]+$`)
)

// Headroom holds the pg_headroom abort ceilings. A zero value is a ceiling the
// target cannot measure (recorded with NotApplicableLocal), never "unlimited".
type Headroom struct {
	MaxConnections   int     `json:"max_connections"`
	MaxLagSeconds    float64 `json:"max_replication_lag_seconds"`
	MaxCPUMillicores int     `json:"max_cpu_millicores"`
	MaxLockWaiters   int     `json:"max_lock_waiters"`
}

// Spec is the validated §2 field set for one seed and leg.
type Spec struct {
	Target             Target
	Digest             string
	IACEnablePR        string
	IACRevertPR        string
	AuthorityDecision  string
	WorkerCounts       int
	WorkerMultiple     int
	LeaseTTL           time.Duration
	HeartbeatInterval  time.Duration
	ReapInterval       time.Duration
	BacklogItems       int
	BacklogScopes      int
	QuotaBudget        int
	Window             time.Duration
	WallClockCap       time.Duration
	StarvationBound    time.Duration
	LockWaitBound      time.Duration
	Headroom           Headroom
	BaselineThroughput string
	Seed               int64
	// Fields is the raw field set as recorded, for the report.
	Fields map[string]string
}

// Workers is W: the production worker count times the multiple (1x or 2x).
func (s Spec) Workers() int { return s.WorkerCounts * s.WorkerMultiple }

// FullSize reports whether the backlog meets the spec floor of 100,000 items
// over at least 200 scopes. A smaller run is a functional proof only.
func (s Spec) FullSize() bool { return s.BacklogItems >= 100_000 && s.BacklogScopes >= 200 }

// fieldReader collects every problem instead of stopping at the first, so
// one run names all blank fields.
type fieldReader struct {
	get    func(string) string
	target Target
	fields map[string]string
	errs   []string
}

func (r *fieldReader) raw(key string) string {
	value := strings.TrimSpace(r.get(key))
	if value == "" {
		r.errs = append(r.errs, key+" is blank")
		return ""
	}
	r.fields[key] = value
	return value
}

// localOnly reads a field that may carry NotApplicableLocal on a local target.
func (r *fieldReader) localOnly(key string) (string, bool) {
	value := r.raw(key)
	if value != NotApplicableLocal {
		return value, false
	}
	if r.target != TargetLocal {
		r.errs = append(r.errs, fmt.Sprintf("%s=%s is only valid on a local target", key, NotApplicableLocal))
	}
	return "", true
}

func (r *fieldReader) positiveInt(key string) int {
	value := r.raw(key)
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		r.errs = append(r.errs, fmt.Sprintf("%s=%q must be a positive integer", key, value))
		return 0
	}
	return n
}

func (r *fieldReader) duration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(r.get(key))
	if value == "" {
		r.fields[key] = fallback.String() + " (default, workflow/defaults.go)"
		return fallback
	}
	r.fields[key] = value
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		r.errs = append(r.errs, fmt.Sprintf("%s=%q must be a positive duration", key, value))
		return 0
	}
	return d
}

func (r *fieldReader) requiredDuration(key string) time.Duration {
	value := r.raw(key)
	if value == "" {
		return 0
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		r.errs = append(r.errs, fmt.Sprintf("%s=%q must be a positive duration", key, value))
		return 0
	}
	return d
}

// LoadSpec reads and validates the §2 fields through get (os.Getenv in the
// entry tests). It fails closed: every blank or malformed field is named in
// the returned error and no partial Spec is usable.
func LoadSpec(get func(string) string) (Spec, error) {
	r := &fieldReader{get: get, fields: map[string]string{}}
	spec := Spec{Fields: r.fields}
	spec.Target = Target(r.raw(EnvTarget))
	switch spec.Target {
	case TargetLocal, TargetLive:
	case "":
	default:
		r.errs = append(r.errs, fmt.Sprintf("%s=%q must be local or live", EnvTarget, spec.Target))
	}
	r.target = spec.Target
	spec.Digest = r.raw(EnvDigest)
	if spec.Digest != "" && !digestPattern.MatchString(spec.Digest) {
		r.errs = append(r.errs, fmt.Sprintf("%s=%q must be a sha256 digest", EnvDigest, spec.Digest))
	}
	spec.IACEnablePR = readPR(r, EnvIACEnablePR)
	spec.IACRevertPR = readPR(r, EnvIACRevertPR)
	spec.AuthorityDecision, _ = r.localOnly(EnvAuthority)
	spec.WorkerCounts = r.positiveInt(EnvWorkerCounts)
	spec.WorkerMultiple = r.positiveInt(EnvWorkerMultiple)
	if spec.WorkerMultiple > 2 {
		r.errs = append(r.errs, fmt.Sprintf("%s=%d must be 1 or 2 (spec §5: 1x and 2x)", EnvWorkerMultiple, spec.WorkerMultiple))
	}
	spec.LeaseTTL = r.duration(EnvLeaseTTL, workflow.DefaultClaimLeaseTTL())
	spec.HeartbeatInterval = r.duration(EnvHeartbeatInterval, workflow.DefaultHeartbeatInterval())
	spec.ReapInterval = r.duration(EnvReapInterval, workflow.DefaultReapInterval())
	if spec.HeartbeatInterval > 0 && spec.HeartbeatInterval >= spec.LeaseTTL {
		r.errs = append(r.errs, fmt.Sprintf("heartbeat interval %s must be below the lease %s", spec.HeartbeatInterval, spec.LeaseTTL))
	}
	spec.BacklogItems = r.positiveInt(EnvBacklogItems)
	spec.BacklogScopes = r.positiveInt(EnvBacklogScopes)
	if spec.BacklogScopes > spec.BacklogItems && spec.BacklogItems > 0 {
		r.errs = append(r.errs, fmt.Sprintf("%s must not exceed %s", EnvBacklogScopes, EnvBacklogItems))
	}
	if quota := r.raw(EnvQuotaBudget); quota != NotApplicable && quota != "" {
		spec.QuotaBudget = r.positiveInt(EnvQuotaBudget)
	}
	spec.Window = time.Duration(r.positiveInt(EnvWindowSeconds)) * time.Second
	spec.WallClockCap = r.requiredDuration(EnvWallClockCap)
	if spec.WallClockCap > maxWallClockCap {
		r.errs = append(r.errs, fmt.Sprintf("%s=%s exceeds the %s cap", EnvWallClockCap, spec.WallClockCap, maxWallClockCap))
	}
	if spec.Window > 0 && spec.WallClockCap > 0 && spec.Window >= spec.WallClockCap {
		r.errs = append(r.errs, fmt.Sprintf("window %s must be below the wall-clock cap %s", spec.Window, spec.WallClockCap))
	}
	spec.StarvationBound = time.Duration(r.positiveInt(EnvStarvationBound)) * time.Second
	spec.LockWaitBound = time.Duration(r.positiveInt(EnvLockWaitBoundMS)) * time.Millisecond
	spec.Headroom = readHeadroom(r)
	spec.BaselineThroughput = r.raw(EnvBaselineThroughput)
	if seed := r.raw(EnvSeed); seed != "" {
		n, err := strconv.ParseInt(seed, 10, 64)
		if err != nil || n <= 0 {
			r.errs = append(r.errs, fmt.Sprintf("%s=%q must be a positive integer", EnvSeed, seed))
		}
		spec.Seed = n
	}
	if spec.Target == TargetLive && strings.TrimSpace(get(EnvLiveGrant)) != LiveGrantComment {
		r.errs = append(r.errs, fmt.Sprintf("%s must equal %s on a live target", EnvLiveGrant, LiveGrantComment))
	}
	if len(r.errs) > 0 {
		sort.Strings(r.errs)
		return Spec{}, errors.New("contention spec §2 fields refused: " + strings.Join(r.errs, "; "))
	}
	return spec, nil
}

func readPR(r *fieldReader, key string) string {
	value, local := r.localOnly(key)
	if local || value == "" {
		return value
	}
	if !prPattern.MatchString(value) {
		r.errs = append(r.errs, fmt.Sprintf("%s=%q must be a PR number", key, value))
	}
	return value
}

func readHeadroom(r *fieldReader) Headroom {
	h := Headroom{MaxConnections: r.positiveInt(EnvPGMaxConnections), MaxLockWaiters: r.positiveInt(EnvPGMaxLockWaiters)}
	if lag, local := r.localOnly(EnvPGMaxLagSeconds); !local && lag != "" {
		v, err := strconv.ParseFloat(lag, 64)
		if err != nil || v <= 0 {
			r.errs = append(r.errs, fmt.Sprintf("%s=%q must be a positive number of seconds", EnvPGMaxLagSeconds, lag))
		}
		h.MaxLagSeconds = v
	}
	if cpu, local := r.localOnly(EnvPGMaxCPUMillicores); !local && cpu != "" {
		v, err := strconv.Atoi(cpu)
		if err != nil || v <= 0 {
			r.errs = append(r.errs, fmt.Sprintf("%s=%q must be positive millicores", EnvPGMaxCPUMillicores, cpu))
		}
		h.MaxCPUMillicores = v
	}
	return h
}
