package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// defaultDependencyReadinessTimeout bounds a single dependency readiness probe.
// It is deliberately shorter than defaultStatusReadinessTimeout so a slow
// dependency surfaces as a bounded readiness failure rather than blocking the
// probe handler. Anti-flap is handled by the Kubernetes probe failureThreshold,
// not by in-process state; see docs/public/reference/health-readiness.md.
const defaultDependencyReadinessTimeout = 2 * time.Second

// ReadinessProbe is a single named dependency check evaluated by /readyz. Each
// probe is run with a bounded timeout and contributes its cause to the
// aggregated readiness failure body when it fails.
type ReadinessProbe struct {
	// Name labels the dependency in the /readyz cause body (e.g. "postgres").
	Name string
	// Timeout bounds this probe; non-positive values fall back to
	// defaultDependencyReadinessTimeout.
	Timeout time.Duration
	// Check reports the dependency error, or nil when the dependency is ready.
	Check func(ctx context.Context) error
}

// combineReadinessProbes builds an AdminCheck that runs every probe
// concurrently, each under its own bounded timeout, and aggregates failures
// into a single, deterministically ordered cause string. It returns nil only
// when every probe reports ready. A probe that panics is reported as a failure
// rather than crashing the probe handler.
func combineReadinessProbes(probes []ReadinessProbe) AdminCheck {
	return func() error {
		if len(probes) == 0 {
			return nil
		}

		failures := make([]string, len(probes))
		var wg sync.WaitGroup
		wg.Add(len(probes))
		for i := range probes {
			go func(i int) {
				defer wg.Done()
				failures[i] = runReadinessProbe(probes[i])
			}(i)
		}
		wg.Wait()

		causes := make([]string, 0, len(failures))
		for _, cause := range failures {
			if cause != "" {
				causes = append(causes, cause)
			}
		}
		if len(causes) == 0 {
			return nil
		}
		sort.Strings(causes)
		return errors.New(strings.Join(causes, "; "))
	}
}

// runReadinessProbe executes one probe under a bounded timeout and returns a
// formatted cause string ("name: error") when it fails, or "" when ready. A
// panic inside the probe is converted to a failure cause so a single broken
// dependency check cannot take down the probe handler.
func runReadinessProbe(probe ReadinessProbe) (cause string) {
	name := strings.TrimSpace(probe.Name)
	if name == "" {
		name = "dependency"
	}
	defer func() {
		if r := recover(); r != nil {
			cause = fmt.Sprintf("%s: panic: %v", name, r)
		}
	}()

	if probe.Check == nil {
		return fmt.Sprintf("%s: no readiness check configured", name)
	}

	timeout := probe.Timeout
	if timeout <= 0 {
		timeout = defaultDependencyReadinessTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := probe.Check(ctx); err != nil {
		return fmt.Sprintf("%s: %v", name, err)
	}
	return ""
}

// statusSnapshotReadinessProbe preserves the original /readyz behavior: it reads
// the storage-backed status snapshot, which exercises Postgres connectivity and
// schema presence. It remains the baseline probe so default callers keep their
// existing readiness contract.
func statusSnapshotReadinessProbe(reader statuspkg.Reader, timeout time.Duration) ReadinessProbe {
	return ReadinessProbe{
		Name:    "status_snapshot",
		Timeout: timeout,
		Check: func(ctx context.Context) error {
			if reader == nil {
				return errors.New("status reader not configured")
			}
			if _, err := reader.ReadStatusSnapshot(ctx, time.Now().UTC()); err != nil {
				return fmt.Errorf("read status snapshot: %w", err)
			}
			return nil
		},
	}
}

// PostgresReadinessProbe verifies Postgres connectivity for /readyz using a
// bounded Ping. A blocked Ping surfaces as a deadline-exceeded cause, which
// distinguishes pool exhaustion or an unreachable database from a schema fault
// reported by the status snapshot probe.
func PostgresReadinessProbe(db *sql.DB, timeout time.Duration) ReadinessProbe {
	return ReadinessProbe{
		Name:    "postgres",
		Timeout: timeout,
		Check: func(ctx context.Context) error {
			if db == nil {
				return errors.New("postgres handle not configured")
			}
			return db.PingContext(ctx)
		},
	}
}

// ReadinessProbesForDependencies builds the standard dependency readiness
// probes for a long-running service, omitting dependencies that are not wired.
// A nil db yields no Postgres probe; a nil graph driver yields no graph probe,
// so the local lightweight profile that disables the graph stays ready. In
// production wiring both handles are non-nil, so both dependencies are probed.
func ReadinessProbesForDependencies(db *sql.DB, driver neo4jdriver.DriverWithContext) []ReadinessProbe {
	var probes []ReadinessProbe
	if db != nil {
		probes = append(probes, PostgresReadinessProbe(db, 0))
	}
	if driver != nil {
		probes = append(probes, GraphReadinessProbe(driver, 0))
	}
	return probes
}

// GraphReadinessProbe verifies graph backend (Bolt) connectivity for /readyz.
// The same Bolt driver fronts both Neo4j and NornicDB, so one probe covers both
// backends. A nil driver (for example the local lightweight profile that
// disables the graph) reports ready so readiness is not gated on a dependency
// the service does not use.
func GraphReadinessProbe(driver neo4jdriver.DriverWithContext, timeout time.Duration) ReadinessProbe {
	return ReadinessProbe{
		Name:    "graph",
		Timeout: timeout,
		Check: func(ctx context.Context) error {
			if driver == nil {
				return nil
			}
			return driver.VerifyConnectivity(ctx)
		},
	}
}
