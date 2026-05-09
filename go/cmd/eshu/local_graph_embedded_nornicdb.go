//go:build nolocalllm

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	nornicauth "github.com/orneryd/nornicdb/pkg/auth"
	nornicbolt "github.com/orneryd/nornicdb/pkg/bolt"
	nornicbuildinfo "github.com/orneryd/nornicdb/pkg/buildinfo"
	nornicconfig "github.com/orneryd/nornicdb/pkg/config"
	norniccypher "github.com/orneryd/nornicdb/pkg/cypher"
	"github.com/orneryd/nornicdb/pkg/nornicdb"
	nornicserver "github.com/orneryd/nornicdb/pkg/server"
	nornicstorage "github.com/orneryd/nornicdb/pkg/storage"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// embeddedLocalNornicDBAvailable reports that this Eshu binary was built with
// the NornicDB library-mode runtime linked in.
func embeddedLocalNornicDBAvailable() bool {
	return true
}

// startEmbeddedLocalNornicDB starts NornicDB in the local Eshu service process while
// exposing the same HTTP and Bolt ports that the process runtime records.
func startEmbeddedLocalNornicDB(ctx context.Context, layout eshulocal.Layout) (*managedLocalGraph, error) {
	boltPort, err := reserveLocalGraphPort()
	if err != nil {
		return nil, err
	}
	httpPort, err := reserveLocalGraphPort()
	if err != nil {
		return nil, err
	}

	dataDir := filepath.Join(layout.GraphDir, "nornicdb")
	logPath := filepath.Join(layout.LogsDir, "graph-nornicdb.log")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create graph data directory: %w", err)
	}
	if err := os.MkdirAll(layout.LogsDir, 0o700); err != nil {
		return nil, fmt.Errorf("create graph logs directory: %w", err)
	}
	credentials, err := loadOrCreateLocalGraphCredentials(filepath.Join(dataDir, "eshu-credentials.json"))
	if err != nil {
		return nil, err
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open graph log file: %w", err)
	}
	restoreStartupOutput, err := redirectEmbeddedNornicDBStartupProcessOutput(logFile)
	if err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("redirect embedded nornicdb startup output: %w", err)
	}
	embedded, err := startEmbeddedNornicDBRuntime(dataDir, localNornicDBBindAddress, boltPort, httpPort, credentials, logFile)
	if err != nil {
		_ = restoreStartupOutput()
		_ = logFile.Close()
		return nil, err
	}

	graph := &managedLocalGraph{
		Backend:  query.GraphBackendNornicDB,
		Version:  nornicbuildinfo.DisplayVersion(),
		Address:  localNornicDBBindAddress,
		BoltPort: boltPort,
		HTTPPort: httpPort,
		DataDir:  dataDir,
		LogPath:  logPath,
		Username: credentials.Username,
		Password: credentials.Password,
		PID:      os.Getpid(),
		logFile:  logFile,
		shutdown: embedded.stop,
	}
	if err := waitForManagedLocalGraph(ctx, graph, localGraphStartupTimeout); err != nil {
		_ = restoreStartupOutput()
		_ = stopManagedLocalGraph(graph, localGraphShutdownTimeout)
		return nil, err
	}
	if err := restoreStartupOutput(); err != nil {
		_ = stopManagedLocalGraph(graph, localGraphShutdownTimeout)
		return nil, fmt.Errorf("restore embedded nornicdb startup output: %w", err)
	}
	return graph, nil
}

type embeddedNornicDBRuntime struct {
	db                    *nornicdb.DB
	httpServer            *nornicserver.Server
	boltServer            *nornicbolt.Server
	restoreStandardLogger func()
}

// startEmbeddedNornicDBRuntime composes NornicDB's public DB, HTTP server, and
// Bolt server APIs into Eshu's local graph lifecycle. The runtime disables
// optional local AI and MCP surfaces so `eshu graph start` only owns graph
// storage for Eshu.
func startEmbeddedNornicDBRuntime(
	dataDir string,
	address string,
	boltPort int,
	httpPort int,
	credentials localGraphCredentials,
	logs io.Writer,
) (_ *embeddedNornicDBRuntime, retErr error) {
	if logs == nil {
		logs = io.Discard
	}
	restoreStandardLogger := redirectEmbeddedNornicDBStandardLogger(logs)
	dbConfig := nornicdb.DefaultConfig()
	dbConfig.Database.DataDir = dataDir
	dbConfig.Database.DefaultDatabase = localNornicDBDefaultDatabase
	dbConfig.Memory.EmbeddingEnabled = false
	dbConfig.Features.HeimdallEnabled = false
	dbConfig.Features.QdrantGRPCEnabled = false

	runtime := &embeddedNornicDBRuntime{restoreStandardLogger: restoreStandardLogger}
	defer func() {
		if retErr != nil {
			_ = runtime.stop(context.Background())
		}
	}()

	db, err := nornicdb.Open(dataDir, dbConfig)
	if err != nil {
		return nil, fmt.Errorf("open embedded nornicdb: %w", err)
	}
	runtime.db = db
	writeEmbeddedNornicDBRuntimeSettings(logs, dbConfig)

	authenticator, err := newEmbeddedNornicDBAuthenticator(db, credentials)
	if err != nil {
		return nil, err
	}

	serverConfig := nornicserver.DefaultConfig()
	serverConfig.Address = address
	serverConfig.Port = httpPort
	serverConfig.MCPEnabled = false
	serverConfig.EmbeddingEnabled = false
	serverConfig.Headless = true
	serverConfig.Features = &nornicconfig.FeatureFlagsConfig{
		HeimdallEnabled:     false,
		QdrantGRPCEnabled:   false,
		SearchRerankEnabled: false,
	}
	httpServer, err := nornicserver.New(db, authenticator, serverConfig)
	if err != nil {
		return nil, fmt.Errorf("create embedded nornicdb http server: %w", err)
	}
	runtime.httpServer = httpServer
	if err = httpServer.Start(); err != nil {
		return nil, fmt.Errorf("start embedded nornicdb http server: %w", err)
	}

	boltConfig := nornicbolt.DefaultConfig()
	boltConfig.Host = address
	boltConfig.Port = boltPort
	boltAuth := nornicbolt.NewAuthenticatorAdapter(authenticator)
	boltAuth.SetGetEffectivePermissions(httpServer.GetEffectivePermissions)
	boltConfig.Authenticator = boltAuth
	boltConfig.RequireAuth = true
	boltServer := nornicbolt.NewWithDatabaseManager(boltConfig, nil, httpServer.GetDatabaseManager())
	boltServer.SetDatabaseAccessModeResolver(httpServer.GetDatabaseAccessModeForRoles)
	boltServer.SetResolvedAccessResolver(httpServer.GetResolvedAccessForRoles)
	runtime.boltServer = boltServer
	go func() {
		if serveErr := boltServer.ListenAndServe(); serveErr != nil {
			_, _ = fmt.Fprintf(logs, "embedded nornicdb bolt server error: %v\n", serveErr)
		}
	}()

	return runtime, nil
}

func writeEmbeddedNornicDBRuntimeSettings(logs io.Writer, config *nornicdb.Config) {
	if logs == nil || config == nil {
		return
	}
	parallel := norniccypher.GetParallelConfig()
	memoryLimit := "unlimited"
	if config.Memory.RuntimeLimit > 0 {
		memoryLimit = nornicconfig.FormatMemorySize(config.Memory.RuntimeLimit)
	}
	_, _ = fmt.Fprintf(
		logs,
		"embedded nornicdb runtime settings: parallel_enabled=%t parallel_workers=%d parallel_min_batch_size=%d memory_limit=%s gc_percent=%d object_pool_enabled=%t object_pool_max_size=%d query_cache_enabled=%t query_cache_size=%d query_cache_ttl=%s embedding_enabled=%t heimdall_enabled=%t qdrant_grpc_enabled=%t\n",
		parallel.Enabled,
		parallel.MaxWorkers,
		parallel.MinBatchSize,
		memoryLimit,
		config.Memory.GCPercent,
		config.Memory.PoolEnabled,
		config.Memory.PoolMaxSize,
		config.Memory.QueryCacheEnabled,
		config.Memory.QueryCacheSize,
		config.Memory.QueryCacheTTL,
		config.Memory.EmbeddingEnabled,
		config.Features.HeimdallEnabled,
		config.Features.QdrantGRPCEnabled,
	)
}

func redirectEmbeddedNornicDBStandardLogger(logs io.Writer) func() {
	if logs == nil {
		logs = io.Discard
	}
	previous := log.Writer()
	log.SetOutput(logs)
	return func() {
		log.SetOutput(previous)
	}
}

func redirectEmbeddedNornicDBStartupProcessOutput(logs io.Writer) (func() error, error) {
	if logs == nil {
		logs = io.Discard
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create embedded nornicdb output pipe: %w", err)
	}
	previousStdout := os.Stdout
	previousStderr := os.Stderr

	var (
		once    sync.Once
		copyErr error
		done    = make(chan struct{})
	)
	go func() {
		defer close(done)
		_, copyErr = io.Copy(logs, reader)
		_ = reader.Close()
	}()

	os.Stdout = writer
	os.Stderr = writer

	return func() error {
		var restoreErr error
		once.Do(func() {
			os.Stdout = previousStdout
			os.Stderr = previousStderr
			restoreErr = writer.Close()
			<-done
			restoreErr = errors.Join(restoreErr, copyErr)
		})
		return restoreErr
	}, nil
}

// newEmbeddedNornicDBAuthenticator gives embedded Bolt and HTTP the same
// workspace-scoped admin user that process mode receives through NORNICDB_AUTH.
func newEmbeddedNornicDBAuthenticator(db *nornicdb.DB, credentials localGraphCredentials) (*nornicauth.Authenticator, error) {
	if db == nil {
		return nil, fmt.Errorf("embedded nornicdb authenticator requires an open database")
	}
	if credentials.Username == "" || credentials.Password == "" {
		return nil, fmt.Errorf("embedded nornicdb authenticator requires username and password")
	}
	authConfig := nornicauth.DefaultAuthConfig()
	authConfig.DefaultAdminUsername = credentials.Username
	authConfig.JWTSecret = []byte(credentials.Password)
	systemStorage := nornicstorage.NewNamespacedEngine(db.GetBaseStorageForManager(), "system")
	authenticator, err := nornicauth.NewAuthenticator(authConfig, systemStorage)
	if err != nil {
		return nil, fmt.Errorf("create embedded nornicdb authenticator: %w", err)
	}
	if _, err := authenticator.CreateUser(credentials.Username, credentials.Password, []nornicauth.Role{nornicauth.RoleAdmin}); err != nil &&
		!errors.Is(err, nornicauth.ErrUserExists) {
		return nil, fmt.Errorf("create embedded nornicdb admin user: %w", err)
	}
	return authenticator, nil
}

// stop shuts down the embedded servers before closing storage so pending Bolt
// or HTTP handlers stop accepting work before the underlying graph files close.
func (r *embeddedNornicDBRuntime) stop(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if r.restoreStandardLogger != nil {
		defer r.restoreStandardLogger()
	}
	var err error
	if r.boltServer != nil {
		err = errors.Join(err, r.boltServer.Close())
	}
	if r.httpServer != nil {
		err = errors.Join(err, r.httpServer.Stop(ctx))
	}
	if r.db != nil {
		r.db.StopEmbedQueue()
		err = errors.Join(err, r.db.Close())
	}
	return err
}
