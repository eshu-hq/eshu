// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// snapshotExportDenialObserver is the optional observer extension that counts
// refused snapshot sets by closed reason (#7957).
type snapshotExportDenialObserver interface {
	ObserveSnapshotExportDenial(ctx context.Context, reason string)
}

// memberSnapshotExportUnqualified excludes one fleet member from the current
// snapshot set after its borrowed connection reported an unqualified build. It
// deliberately does not wrap db.ErrSnapshotExportUnqualified: a qualified
// sibling may still serve the set, so the request is refused only when every
// eligible member was excluded this way.
type memberSnapshotExportUnqualified struct{}

func (memberSnapshotExportUnqualified) Error() string {
	return "PostgreSQL reader member build is not qualified for snapshot export"
}

// snapshotExportAvailable reports whether any reader target can serve a
// snapshot set of count connections on a qualified build. It reads only the
// atomic verdicts, so the refusal costs no borrow, gate wait, or reservation.
func (a *Access) snapshotExportAvailable(count int) bool {
	if len(a.readerMembers) == 0 {
		return a.snapshotExport.qualified()
	}
	for i := range a.readerMembers {
		member := &a.readerMembers[i]
		if member.maxOpen >= count && member.snapshotExport.qualified() {
			return true
		}
	}
	return false
}

// snapshotExportMembers filters a snapshot-set member order, in place, to the
// members whose verdict is qualified. Single-connection reads never call it:
// only sets export.
func (a *Access) snapshotExportMembers(order []int) []int {
	kept := order[:0]
	for _, index := range order {
		if a.readerMembers[index].snapshotExport.qualified() {
			kept = append(kept, index)
		}
	}
	return kept
}

// refuseSnapshotExport records one refused snapshot set (the denial counter
// and a WARN record per reader target with its build evidence) and returns the
// permanent sentinel. It never touches a connection.
func (a *Access) refuseSnapshotExport(ctx context.Context) error {
	reason := a.snapshotExport.denialReason()
	if len(a.readerMembers) > 0 {
		reason = a.readerMembers[0].snapshotExport.denialReason()
	}
	if observer, ok := a.observer.(snapshotExportDenialObserver); ok {
		observer.ObserveSnapshotExportDenial(ctx, reason)
	}
	const message = "shared-snapshot read refused: PostgreSQL reader build is not qualified for snapshot export"
	if len(a.readerMembers) == 0 {
		logSnapshotExportVerdict(ctx, a.logger, slog.LevelWarn, snapshotExportDeniedEvent, message, -1, a.snapshotExport)
	} else {
		for i := range a.readerMembers {
			member := &a.readerMembers[i]
			logSnapshotExportVerdict(ctx, a.logger, slog.LevelWarn, snapshotExportDeniedEvent, message, member.ordinal, member.snapshotExport)
		}
	}
	return db.ErrSnapshotExportUnqualified
}
