package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
	"github.com/jackc/pgx/v5"
)

func validSnapshotID(id string) bool {
	if id == "" {
		return false
	}
	for _, char := range id {
		if !strings.ContainsRune("0123456789abcdefABCDEF-", char) {
			return false
		}
	}
	return true
}

func runConcurrentGroups(ctx context.Context, txs []pgx.Tx, groups [][]string) (string, int, time.Duration, time.Duration, time.Duration, error) {
	groupTimes := make([]time.Duration, len(groups))
	started := time.Now()
	parts, err := codetopicparallel.RunPartitions(ctx, len(groups), func(workerCtx context.Context, index int) ([]codetopicparallel.ProbeRow, error) {
		rows, elapsed, queryErr := runGroup(workerCtx, txs[index], groups[index])
		groupTimes[index] = elapsed
		return rows, queryErr
	})
	if err != nil {
		return "", 0, 0, 0, 0, fmt.Errorf("run concurrent proof groups: %w", err)
	}
	probeWall := time.Since(started)
	rows := make([]codetopicparallel.ProbeRow, 0, 8000)
	for _, part := range parts {
		rows = append(rows, part...)
	}
	assemblyStarted := time.Now()
	pageHash, err := assemblyHash(ctx, txs[0], rows)
	if err != nil {
		return "", 0, 0, 0, 0, err
	}
	assemblyWall := time.Since(assemblyStarted)
	var maxGroup time.Duration
	for _, elapsed := range groupTimes {
		if elapsed > maxGroup {
			maxGroup = elapsed
		}
	}
	return pageHash, len(rows), time.Since(started), probeWall, maxGroup + assemblyWall, nil
}

func runParallel(ctx context.Context, config *pgx.ConnConfig, candidateCount int) error {
	started := time.Now()
	connections := make([]*pgx.Conn, 0, candidateCount)
	defer func() {
		for _, conn := range connections {
			_ = conn.Close(context.Background())
		}
	}()
	for range candidateCount {
		conn, err := pgx.ConnectConfig(ctx, config.Copy())
		if err != nil {
			return fmt.Errorf("connect proof reader: %w", err)
		}
		connections = append(connections, conn)
	}
	connectWall := time.Since(started)
	txs := make([]pgx.Tx, 0, candidateCount)
	defer func() {
		for _, tx := range txs {
			_ = tx.Rollback(context.Background())
		}
	}()
	for _, conn := range connections {
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return fmt.Errorf("begin proof snapshot: %w", err)
		}
		txs = append(txs, tx)
	}
	var snapshotID string
	if err := txs[0].QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&snapshotID); err != nil {
		return fmt.Errorf("export snapshot: %w", err)
	}
	if !validSnapshotID(snapshotID) {
		return fmt.Errorf("invalid exported snapshot identifier")
	}
	for _, tx := range txs[1:] {
		if _, err := tx.Exec(ctx, "SET TRANSACTION SNAPSHOT '"+snapshotID+"'"); err != nil {
			return fmt.Errorf("import snapshot: %w", err)
		}
	}
	for _, tx := range txs {
		if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '5s'"); err != nil {
			return fmt.Errorf("bound statement: %w", err)
		}
	}
	setupWall := time.Since(started) - connectWall
	fmt.Printf("parallel_setup connect_ms=%.3f snapshot_ms=%.3f\n", float64(connectWall.Microseconds())/1000, float64(setupWall.Microseconds())/1000)
	modes := []string{"baseline", "balanced", "balanced", "baseline"}
	var firstHash string
	for i, mode := range modes {
		groups := partitionTerms(terms, 4)
		if mode == "balanced" {
			if candidateCount == 8 {
				groups = partitionTerms(terms, 8)
			} else {
				groups = balancedGroups()
			}
		}
		hash, count, wall, probe, combined, err := runConcurrentGroups(ctx, txs, groups)
		if err != nil {
			return fmt.Errorf("%s request %d: %w", mode, i, err)
		}
		if i == 0 {
			firstHash = hash
		}
		fmt.Printf("parallel_request=%d mode=%s readers=%d rows=%d page_equal=%t wall_ms=%.3f probe_ms=%.3f max_group_plus_assembly_ms=%.3f\n",
			i, mode, len(groups), count, hash == firstHash, float64(wall.Microseconds())/1000,
			float64(probe.Microseconds())/1000, float64(combined.Microseconds())/1000)
		if hash != firstHash || count != 8000 {
			return fmt.Errorf("response changed; stopping")
		}
	}
	fmt.Printf("parallel_snapshot_age_ms=%d\n", time.Since(started).Milliseconds())
	return nil
}
