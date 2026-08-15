// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package localsupervisor

import "time"

// How long the supervisor waits on a local graph backend, plus the settings
// that decide where that backend listens, who it authenticates as, and which
// runtime it uses. graph.go, graph_process.go, graph_embedded_nornicdb.go,
// host.go, and lifecycle.go all read from here.
const (
	// localGraphStartupTimeout bounds how long a just-started backend has to
	// answer both health probes. Overrunning it fails the start and points the
	// operator at the backend log.
	localGraphStartupTimeout = 45 * time.Second

	// GraphHealthTimeout bounds one health probe: the HTTP GET /health request,
	// and separately the Bolt dial plus its handshake reply. A probe that
	// overruns it counts as unhealthy. It is a per-attempt bound, not a
	// readiness budget — startup and stop both retry probes against their own
	// deadlines. `eshu vuln scan` reuses it as the per-request timeout while it
	// polls the local API's /healthz.
	GraphHealthTimeout = 1 * time.Second

	// GraphShutdownTimeout bounds one backend stop. StopManagedGraph gives the
	// in-process shutdown hook, or a SIGTERMed child, this long before killing
	// it; stopping a backend recorded by another owner waits this long for it to
	// stop answering health probes; and `eshu graph stop` reuses it as the
	// deadline for the owner, or its backend, to go away before reporting that
	// it did not stop.
	GraphShutdownTimeout = 10 * time.Second

	// localNornicDBBindAddress is the loopback address a managed backend binds,
	// that ports are reserved on, and that health probes fall back to when an
	// owner record carries no address. A local backend is never reachable
	// off-host.
	localNornicDBBindAddress = "127.0.0.1"

	// localNornicDBAdminUsername is the account name the generated workspace
	// credentials are written under, and the fallback used when a record carries
	// a password but no username.
	localNornicDBAdminUsername = "admin"

	// GraphDatabaseName is the single database inside the local backend that
	// everything opens. The supervisor configures the backend with it
	// (NORNICDB_DEFAULT_DATABASE for a managed child, DefaultDatabase for the
	// in-process runtime) and hands it to child services as ESHU_NEO4J_DATABASE,
	// NEO4J_DATABASE, and DEFAULT_DATABASE. Code that opens its own Bolt session
	// against a supervised backend — the cmd/eshu local-authoritative perf tests
	// and NornicDB compatibility tests — has to name the same database.
	GraphDatabaseName = "nornic"

	// localNornicDBRuntimeModeEnv picks the backend runtime: unset or "embedded"
	// uses the NornicDB runtime linked into this binary, "process" spawns a
	// managed child instead. Any other value is rejected, and "embedded" on a
	// build with no runtime linked in is an error rather than a silent fallback
	// to a child process.
	localNornicDBRuntimeModeEnv = "ESHU_NORNICDB_RUNTIME"
)
