package runtime

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jconfig "github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
)

const (
	defaultPostgresMaxOpenConns              = 30
	defaultPostgresMaxIdleConns              = 10
	defaultPostgresConnMaxLifetime           = 30 * time.Minute
	defaultPostgresConnMaxIdleTime           = 10 * time.Minute
	defaultPostgresPingTimeout               = 10 * time.Second
	defaultNornicDBDatabaseName              = "nornic"
	defaultNeo4jDatabaseName                 = "neo4j"
	defaultNeo4jMaxConnectionPoolSize        = 100
	defaultNeo4jMaxConnectionLifetime        = time.Hour
	defaultNeo4jConnectionAcquisitionTimeout = time.Minute
	defaultNeo4jSocketConnectTimeout         = 5 * time.Second
	defaultNeo4jVerifyTimeout                = 10 * time.Second
)

// GraphBackend names the graph database adapter selected for a Eshu runtime.
type GraphBackend string

const (
	// GraphBackendNeo4j selects the official Neo4j graph adapter.
	GraphBackendNeo4j GraphBackend = "neo4j"
	// GraphBackendNornicDB selects the official NornicDB graph adapter.
	GraphBackendNornicDB GraphBackend = "nornicdb"
)

var postgresDSNEnvKeys = []string{
	"ESHU_FACT_STORE_DSN",
	"ESHU_CONTENT_STORE_DSN",
	"ESHU_POSTGRES_DSN",
}

// PostgresConfig captures the shared database and pool tuning used by Go
// services that talk to Postgres.
type PostgresConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	PingTimeout     time.Duration
}

// PostgresPoolSetter is the minimal tuning surface required from sql.DB.
type PostgresPoolSetter interface {
	SetMaxOpenConns(int)
	SetMaxIdleConns(int)
	SetConnMaxLifetime(time.Duration)
	SetConnMaxIdleTime(time.Duration)
}

// Neo4jConfig captures shared driver and pool tuning for Go services that talk
// to Neo4j.
type Neo4jConfig struct {
	URI                          string
	Username                     string
	Password                     string
	DatabaseName                 string
	MaxConnectionPoolSize        int
	MaxConnectionLifetime        time.Duration
	ConnectionAcquisitionTimeout time.Duration
	SocketConnectTimeout         time.Duration
	VerifyTimeout                time.Duration
}

// LoadGraphBackend validates the selected graph backend for the current
// process. Empty uses the NornicDB default.
func LoadGraphBackend(getenv func(string) string) (GraphBackend, error) {
	switch GraphBackend(strings.TrimSpace(getenv("ESHU_GRAPH_BACKEND"))) {
	case "":
		return GraphBackendNornicDB, nil
	case GraphBackendNeo4j:
		return GraphBackendNeo4j, nil
	case GraphBackendNornicDB:
		return GraphBackendNornicDB, nil
	default:
		return "", fmt.Errorf("invalid ESHU_GRAPH_BACKEND %q", strings.TrimSpace(getenv("ESHU_GRAPH_BACKEND")))
	}
}

// LoadPostgresConfig reads the shared Postgres config from env.
func LoadPostgresConfig(getenv func(string) string) (PostgresConfig, error) {
	dsn := firstEnvValue(getenv, postgresDSNEnvKeys...)
	if dsn == "" {
		return PostgresConfig{}, fmt.Errorf("set ESHU_FACT_STORE_DSN, ESHU_CONTENT_STORE_DSN, or ESHU_POSTGRES_DSN")
	}

	maxOpenConns, err := intEnvOrDefault(
		getenv,
		"ESHU_POSTGRES_MAX_OPEN_CONNS",
		defaultPostgresMaxOpenConns,
	)
	if err != nil {
		return PostgresConfig{}, err
	}
	maxIdleConns, err := intEnvOrDefault(
		getenv,
		"ESHU_POSTGRES_MAX_IDLE_CONNS",
		defaultPostgresMaxIdleConns,
	)
	if err != nil {
		return PostgresConfig{}, err
	}
	connMaxLifetime, err := durationEnvOrDefault(
		getenv,
		"ESHU_POSTGRES_CONN_MAX_LIFETIME",
		defaultPostgresConnMaxLifetime,
	)
	if err != nil {
		return PostgresConfig{}, err
	}
	connMaxIdleTime, err := durationEnvOrDefault(
		getenv,
		"ESHU_POSTGRES_CONN_MAX_IDLE_TIME",
		defaultPostgresConnMaxIdleTime,
	)
	if err != nil {
		return PostgresConfig{}, err
	}
	pingTimeout, err := durationEnvOrDefault(
		getenv,
		"ESHU_POSTGRES_PING_TIMEOUT",
		defaultPostgresPingTimeout,
	)
	if err != nil {
		return PostgresConfig{}, err
	}

	cfg := PostgresConfig{
		DSN:             dsn,
		MaxOpenConns:    maxOpenConns,
		MaxIdleConns:    maxIdleConns,
		ConnMaxLifetime: connMaxLifetime,
		ConnMaxIdleTime: connMaxIdleTime,
		PingTimeout:     pingTimeout,
	}

	if cfg.MaxOpenConns <= 0 {
		return PostgresConfig{}, fmt.Errorf("ESHU_POSTGRES_MAX_OPEN_CONNS must be positive")
	}
	if cfg.MaxIdleConns < 0 {
		return PostgresConfig{}, fmt.Errorf("ESHU_POSTGRES_MAX_IDLE_CONNS must be zero or positive")
	}
	if cfg.MaxIdleConns > cfg.MaxOpenConns {
		cfg.MaxIdleConns = cfg.MaxOpenConns
	}

	return cfg, nil
}

// ConfigurePostgresPool applies the shared Postgres pool policy to a target.
func ConfigurePostgresPool(target PostgresPoolSetter, cfg PostgresConfig) {
	target.SetMaxOpenConns(cfg.MaxOpenConns)
	target.SetMaxIdleConns(cfg.MaxIdleConns)
	target.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	target.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
}

// OpenPostgres opens, tunes, and verifies a Postgres connection for a Go
// service runtime.
func OpenPostgres(ctx context.Context, getenv func(string) string) (*sql.DB, error) {
	cfg, err := LoadPostgresConfig(getenv)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}
	ConfigurePostgresPool(db, cfg)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return db, nil
}

// LoadNeo4jConfig reads the shared Neo4j config from env.
func LoadNeo4jConfig(getenv func(string) string) (Neo4jConfig, error) {
	maxConnectionPoolSize, err := intEnvOrDefault(
		getenv,
		"ESHU_NEO4J_MAX_CONNECTION_POOL_SIZE",
		defaultNeo4jMaxConnectionPoolSize,
	)
	if err != nil {
		return Neo4jConfig{}, err
	}
	maxConnectionLifetime, err := durationEnvOrDefault(
		getenv,
		"ESHU_NEO4J_MAX_CONNECTION_LIFETIME",
		defaultNeo4jMaxConnectionLifetime,
	)
	if err != nil {
		return Neo4jConfig{}, err
	}
	connectionAcquisitionTimeout, err := durationEnvOrDefault(
		getenv,
		"ESHU_NEO4J_CONNECTION_ACQUISITION_TIMEOUT",
		defaultNeo4jConnectionAcquisitionTimeout,
	)
	if err != nil {
		return Neo4jConfig{}, err
	}
	socketConnectTimeout, err := durationEnvOrDefault(
		getenv,
		"ESHU_NEO4J_SOCKET_CONNECT_TIMEOUT",
		defaultNeo4jSocketConnectTimeout,
	)
	if err != nil {
		return Neo4jConfig{}, err
	}
	verifyTimeout, err := durationEnvOrDefault(
		getenv,
		"ESHU_NEO4J_VERIFY_TIMEOUT",
		defaultNeo4jVerifyTimeout,
	)
	if err != nil {
		return Neo4jConfig{}, err
	}

	cfg := Neo4jConfig{
		URI:                          firstEnvValue(getenv, "ESHU_NEO4J_URI", "NEO4J_URI"),
		Username:                     firstEnvValue(getenv, "ESHU_NEO4J_USERNAME", "NEO4J_USERNAME"),
		Password:                     firstEnvValue(getenv, "ESHU_NEO4J_PASSWORD", "NEO4J_PASSWORD"),
		DatabaseName:                 firstEnvValue(getenv, "ESHU_NEO4J_DATABASE", "NEO4J_DATABASE"),
		MaxConnectionPoolSize:        maxConnectionPoolSize,
		MaxConnectionLifetime:        maxConnectionLifetime,
		ConnectionAcquisitionTimeout: connectionAcquisitionTimeout,
		SocketConnectTimeout:         socketConnectTimeout,
		VerifyTimeout:                verifyTimeout,
	}
	if cfg.DatabaseName == "" {
		graphBackend, err := LoadGraphBackend(getenv)
		if err != nil {
			return Neo4jConfig{}, err
		}
		if graphBackend == GraphBackendNeo4j {
			cfg.DatabaseName = defaultNeo4jDatabaseName
		} else {
			cfg.DatabaseName = defaultNornicDBDatabaseName
		}
	}
	if cfg.URI == "" || cfg.Username == "" || cfg.Password == "" {
		return Neo4jConfig{}, fmt.Errorf(
			"set ESHU_NEO4J_URI/NEO4J_URI, ESHU_NEO4J_USERNAME/NEO4J_USERNAME, and ESHU_NEO4J_PASSWORD/NEO4J_PASSWORD",
		)
	}
	if cfg.MaxConnectionPoolSize == 0 {
		return Neo4jConfig{}, fmt.Errorf("ESHU_NEO4J_MAX_CONNECTION_POOL_SIZE must not be zero")
	}

	return cfg, nil
}

// ApplyNeo4jConfig applies the shared Neo4j tuning policy to a driver config.
func ApplyNeo4jConfig(target *neo4jconfig.Config, cfg Neo4jConfig) {
	target.MaxConnectionPoolSize = cfg.MaxConnectionPoolSize
	target.MaxConnectionLifetime = cfg.MaxConnectionLifetime
	target.ConnectionAcquisitionTimeout = cfg.ConnectionAcquisitionTimeout
	target.SocketConnectTimeout = cfg.SocketConnectTimeout
}

// OpenNeo4jDriver opens and verifies a Neo4j driver with shared pool tuning.
func OpenNeo4jDriver(
	ctx context.Context,
	getenv func(string) string,
) (neo4jdriver.DriverWithContext, Neo4jConfig, error) {
	graphBackend, err := LoadGraphBackend(getenv)
	if err != nil {
		return nil, Neo4jConfig{}, err
	}
	if graphBackend != GraphBackendNeo4j && graphBackend != GraphBackendNornicDB {
		return nil, Neo4jConfig{}, fmt.Errorf(
			"graph backend %q is not wired for the shared Bolt driver path",
			graphBackend,
		)
	}

	cfg, err := LoadNeo4jConfig(getenv)
	if err != nil {
		return nil, Neo4jConfig{}, err
	}

	driver, err := neo4jdriver.NewDriverWithContext(
		cfg.URI,
		neo4jdriver.BasicAuth(cfg.Username, cfg.Password, ""),
		func(driverCfg *neo4jconfig.Config) {
			ApplyNeo4jConfig(driverCfg, cfg)
		},
	)
	if err != nil {
		return nil, Neo4jConfig{}, fmt.Errorf("open neo4j driver: %w", err)
	}

	verifyCtx, cancel := context.WithTimeout(ctx, cfg.VerifyTimeout)
	defer cancel()
	if err := driver.VerifyConnectivity(verifyCtx); err != nil {
		_ = driver.Close(context.Background())
		return nil, Neo4jConfig{}, fmt.Errorf("verify neo4j connectivity: %w", err)
	}

	return driver, cfg, nil
}

func firstEnvValue(getenv func(string) string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
	}

	return ""
}

func intEnvOrDefault(getenv func(string) string, name string, fallback int) (int, error) {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}

	return parsed, nil
}

func durationEnvOrDefault(
	getenv func(string) string,
	name string,
	fallback time.Duration,
) (time.Duration, error) {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}

	return parsed, nil
}
