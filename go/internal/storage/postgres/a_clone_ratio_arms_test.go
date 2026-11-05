// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
)

// Concurrent-clone and durability-off arms of the #10148 clone-ratio probe; see
// a_clone_ratio_probe_test.go for the whole probe.

// probeConcurrent clones one template several times at once, as the gate's
// parallel tests would, and checks each clone against the template catalog.
func probeConcurrent(t *testing.T, e probeEnv, flavour string, template *probeBuilt, searchPath string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	allOK := true
	for run := 0; run < probeConcurrentRuns; run++ {
		names := make([]string, probeConcurrentSize)
		took := make([]time.Duration, probeConcurrentSize)
		errs := make([]error, probeConcurrentSize)
		var wg sync.WaitGroup
		start := time.Now()
		for k := range names {
			names[k] = probeDatabaseName(t)
			wg.Add(1)
			go func() {
				defer wg.Done()
				s := time.Now()
				errs[k] = e.createClone(ctx, template.name, names[k])
				took[k] = time.Since(s)
			}()
		}
		wg.Wait()
		wall := time.Since(start)
		failed, equal := 0, 0
		var states []string
		for k, name := range names {
			if errs[k] != nil {
				failed++
				states = append(states, probeSQLState(errs[k]))
				continue
			}
			db := e.connect(name, searchPath)
			if fp, err := probeFingerprint(ctx, db); err == nil {
				if ok, _ := probeCompare(template.catalog, fp); ok {
					equal++
				}
			}
			_ = db.Close()
		}
		each := make([]string, len(took))
		for k, d := range took {
			each[k] = probeMS(d)
			e.dropDatabase(names[k])
		}
		ok := failed == 0 && equal == probeConcurrentSize
		allOK = allOK && ok
		probeLine(t, "kind=concurrent flavour=%s run=%d clones=%d wall_ms=%s each_ms=%s failed=%d sqlstates=%q equal_to_template=%d ok=%t", flavour, run, probeConcurrentSize, probeMS(wall), strings.Join(each, "/"), failed, strings.Join(states, ","), equal, ok)
	}
	// A session on the template must make the clone fail with SQLSTATE 55006.
	busy := e.connect(template.name, searchPath)
	defer func() { _ = busy.Close() }()
	if err := busy.PingContext(ctx); err == nil {
		name := probeDatabaseName(t)
		err = e.createClone(ctx, template.name, name)
		probeLine(t, "kind=busy_template flavour=%s sqlstate=%s", flavour, probeSQLState(err))
		e.dropDatabase(name)
	}
	return allOK
}

// durability reads fsync, synchronous_commit and full_page_writes on a fresh
// session so a reload shows up.
func (e probeEnv) durability(ctx context.Context) (string, error) {
	db := stdlib.OpenDB(*e.cfg.Copy())
	defer func() { _ = db.Close() }()
	var s string
	err := db.QueryRowContext(ctx, `SELECT string_agg(name||'='||setting||'('||source||')', ';' ORDER BY name) FROM pg_settings WHERE name IN ('fsync','synchronous_commit','full_page_writes')`).Scan(&s)
	return s, err
}

// setDurability turns the three settings off (ALTER SYSTEM SET) or resets them,
// reloads, and waits until a fresh session shows the wanted state.
func (e probeEnv) setDurability(ctx context.Context, off bool, want string) error {
	db := stdlib.OpenDB(*e.cfg.Copy())
	defer func() { _ = db.Close() }()
	for _, name := range []string{"fsync", "synchronous_commit", "full_page_writes"} {
		stmt := "ALTER SYSTEM RESET " + name
		if off {
			stmt = "ALTER SYSTEM SET " + name + " = off"
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	if _, err := db.ExecContext(ctx, `SELECT pg_reload_conf()`); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		got, err := e.durability(ctx)
		if err != nil {
			return err
		}
		isOff := strings.Contains(got, "fsync=off") && strings.Contains(got, "full_page_writes=off") && strings.Contains(got, "synchronous_commit=off")
		if (off && isOff) || (!off && got == want) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("settings did not reach the wanted state in 15s: %s (want %q)", got, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
