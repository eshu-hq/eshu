// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// repoDependencyLeaseReleaseTimeout bounds the release that runs after the
// runner's own context has been cancelled: long enough for one Postgres
// round trip on a loaded host, short enough that shutdown cannot hang on a
// dead backend.
const repoDependencyLeaseReleaseTimeout = 10 * time.Second

// errRepoDependencyShutdown marks a cycle cut short by the runner's own
// context ending before anything could have been mutated. runSerial treats it
// as a clean stop rather than a cycle failure.
var errRepoDependencyShutdown = errors.New("repo dependency projection cycle interrupted by shutdown")

// repoDependencyShutdownError wraps both the shutdown marker and the
// cancellation so callers can errors.Is either. It never wraps a quarantine:
// the process is going away, so holding the partition for the lease TTL
// would only strand the next owner (#6747).
func repoDependencyShutdownError(ctx context.Context) error {
	return fmt.Errorf("%w: %w", errRepoDependencyShutdown, ctx.Err())
}

// failCycle classifies a cycle error against the runner's parent context for
// the phases that cannot have mutated anything: the selection scan, the
// empty-cycle exit, and the missing-gate exit. A parent cancellation there is
// shutdown: the caller releases its partition lease and returns the
// cancellation. Everything else keeps the fail-closed quarantine, which
// deliberately holds the lease for the TTL so an uncertain owner cannot
// re-enter before the backend has quiesced. Errors from inside or after the
// acceptance-unit gate never go through here: once the gate has opened, a
// cancelled graph write or an ambiguous Postgres commit may still be
// settling, and evidence-5122-repo-dependency-safety-proof.md reserves the
// lease TTL as that quiescence window even across a process stop.
func (r *RepoDependencyProjectionRunner) failCycle(ctx context.Context, err error, releaseLease *bool) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		*releaseLease = true
		return repoDependencyShutdownError(ctx)
	}
	return r.quarantineLease(err)
}

// releasePartitionLease releases this runner's partition lease through a
// context that survives the runner's own cancellation. Releasing through the
// cancelled context handed Postgres an already-dead request, the error was
// swallowed, and the lease sat held for its full TTL under an owner that no
// longer existed; the next reducer process could not claim the partition and
// its repo_dependency intents stayed nonterminal for that long (#6747 shape
// A, Ifa runs 35165747820 and 35200501470).
func (r *RepoDependencyProjectionRunner) releasePartitionLease(ctx context.Context) {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), repoDependencyLeaseReleaseTimeout)
	defer cancel()
	err := r.LeaseManager.ReleasePartitionLease(
		releaseCtx,
		DomainRepoDependency,
		r.Config.partitionID(),
		r.Config.partitionCount(),
		r.Config.leaseOwner(),
	)
	if err != nil && r.Logger != nil {
		r.Logger.WarnContext(
			releaseCtx,
			"repo dependency partition lease release failed; the lease expires on its TTL",
			slog.Int("partition_id", r.Config.partitionID()),
			slog.Int("partition_count", r.Config.partitionCount()),
			slog.String("lease_owner", r.Config.leaseOwner()),
			slog.Float64("lease_ttl_seconds", r.Config.leaseTTL().Seconds()),
			log.Err(err),
			telemetry.PhaseAttr(telemetry.PhaseReduction),
		)
	}
}

// repoDependencyLeaseQuarantineError keeps an uncertain shard owner from
// re-entering before every canceled or ambiguous graph transaction has had the
// full lease window to quiesce.
type repoDependencyLeaseQuarantineError struct {
	delay time.Duration
	cause error
}

func (e *repoDependencyLeaseQuarantineError) Error() string {
	return fmt.Sprintf("quarantine repo dependency lease for %s: %v", e.delay, e.cause)
}

func (e *repoDependencyLeaseQuarantineError) Unwrap() error {
	return e.cause
}

func repoDependencyQuarantineDelay(err error, fallback time.Duration) time.Duration {
	var quarantineErr *repoDependencyLeaseQuarantineError
	if err != nil && errors.As(err, &quarantineErr) && quarantineErr.delay > fallback {
		return quarantineErr.delay
	}
	return fallback
}

func repoDependencyLeaseQuarantineReason(err error) string {
	var quarantineErr *repoDependencyLeaseQuarantineError
	if !errors.As(err, &quarantineErr) {
		return "not_quarantined"
	}
	if errors.Is(quarantineErr.cause, context.DeadlineExceeded) {
		return "cycle_deadline"
	}
	if strings.Contains(strings.ToLower(quarantineErr.cause.Error()), "heartbeat") {
		return "heartbeat_lost"
	}
	return "cycle_error"
}
