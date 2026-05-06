package runtime

import (
	"testing"
	"time"

	neo4jconfig "github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
)

func TestLoadPostgresConfigUsesDefaultsAndClampsIdlePool(t *testing.T) {
	t.Parallel()

	cfg, err := LoadPostgresConfig(func(key string) string {
		switch key {
		case "ESHU_FACT_STORE_DSN":
			return "postgresql://eshu:change-me@localhost:15432/eshu"
		case "ESHU_POSTGRES_MAX_OPEN_CONNS":
			return "8"
		case "ESHU_POSTGRES_MAX_IDLE_CONNS":
			return "12"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadPostgresConfig() error = %v, want nil", err)
	}
	if got, want := cfg.MaxOpenConns, 8; got != want {
		t.Fatalf("MaxOpenConns = %d, want %d", got, want)
	}
	if got, want := cfg.MaxIdleConns, 8; got != want {
		t.Fatalf("MaxIdleConns = %d, want %d", got, want)
	}
	if got, want := cfg.ConnMaxLifetime, defaultPostgresConnMaxLifetime; got != want {
		t.Fatalf("ConnMaxLifetime = %v, want %v", got, want)
	}
	if got, want := cfg.ConnMaxIdleTime, defaultPostgresConnMaxIdleTime; got != want {
		t.Fatalf("ConnMaxIdleTime = %v, want %v", got, want)
	}
	if got, want := cfg.PingTimeout, defaultPostgresPingTimeout; got != want {
		t.Fatalf("PingTimeout = %v, want %v", got, want)
	}
}

func TestLoadPostgresConfigRejectsMissingDSN(t *testing.T) {
	t.Parallel()

	_, err := LoadPostgresConfig(func(string) string { return "" })
	if err == nil {
		t.Fatal("LoadPostgresConfig() error = nil, want non-nil")
	}
}

func TestLoadPostgresConfigRejectsInvalidPoolTuning(t *testing.T) {
	t.Parallel()

	_, err := LoadPostgresConfig(func(key string) string {
		switch key {
		case "ESHU_FACT_STORE_DSN":
			return "postgresql://eshu:change-me@localhost:15432/eshu"
		case "ESHU_POSTGRES_MAX_OPEN_CONNS":
			return "not-a-number"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("LoadPostgresConfig() error = nil, want non-nil")
	}
}

func TestConfigurePostgresPoolAppliesSharedSettings(t *testing.T) {
	t.Parallel()

	target := &recordingPostgresPoolSetter{}
	cfg := PostgresConfig{
		MaxOpenConns:    15,
		MaxIdleConns:    6,
		ConnMaxLifetime: 45 * time.Minute,
		ConnMaxIdleTime: 12 * time.Minute,
	}

	ConfigurePostgresPool(target, cfg)

	if got, want := target.maxOpenConns, 15; got != want {
		t.Fatalf("maxOpenConns = %d, want %d", got, want)
	}
	if got, want := target.maxIdleConns, 6; got != want {
		t.Fatalf("maxIdleConns = %d, want %d", got, want)
	}
	if got, want := target.connMaxLifetime, 45*time.Minute; got != want {
		t.Fatalf("connMaxLifetime = %v, want %v", got, want)
	}
	if got, want := target.connMaxIdleTime, 12*time.Minute; got != want {
		t.Fatalf("connMaxIdleTime = %v, want %v", got, want)
	}
}

func TestLoadNeo4jConfigUsesDefaultsAndOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := LoadNeo4jConfig(func(key string) string {
		switch key {
		case "NEO4J_URI":
			return "bolt://localhost:7687"
		case "NEO4J_USERNAME":
			return "neo4j"
		case "NEO4J_PASSWORD":
			return "change-me"
		case "ESHU_NEO4J_MAX_CONNECTION_POOL_SIZE":
			return "33"
		case "ESHU_NEO4J_CONNECTION_ACQUISITION_TIMEOUT":
			return "45s"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadNeo4jConfig() error = %v, want nil", err)
	}
	if got, want := cfg.DatabaseName, defaultNornicDBDatabaseName; got != want {
		t.Fatalf("DatabaseName = %q, want %q", got, want)
	}
	if got, want := cfg.MaxConnectionPoolSize, 33; got != want {
		t.Fatalf("MaxConnectionPoolSize = %d, want %d", got, want)
	}
	if got, want := cfg.ConnectionAcquisitionTimeout, 45*time.Second; got != want {
		t.Fatalf("ConnectionAcquisitionTimeout = %v, want %v", got, want)
	}
}

func TestLoadNeo4jConfigDefaultsDatabaseForExplicitNeo4jBackend(t *testing.T) {
	t.Parallel()

	cfg, err := LoadNeo4jConfig(func(key string) string {
		switch key {
		case "ESHU_GRAPH_BACKEND":
			return "neo4j"
		case "NEO4J_URI":
			return "bolt://localhost:7687"
		case "NEO4J_USERNAME":
			return "neo4j"
		case "NEO4J_PASSWORD":
			return "change-me"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadNeo4jConfig() error = %v, want nil", err)
	}
	if got, want := cfg.DatabaseName, defaultNeo4jDatabaseName; got != want {
		t.Fatalf("DatabaseName = %q, want %q", got, want)
	}
}

func TestLoadGraphBackendDefaultsToNornicDB(t *testing.T) {
	t.Parallel()

	got, err := LoadGraphBackend(func(string) string { return "" })
	if err != nil {
		t.Fatalf("LoadGraphBackend() error = %v, want nil", err)
	}
	if got != GraphBackendNornicDB {
		t.Fatalf("LoadGraphBackend() = %q, want %q", got, GraphBackendNornicDB)
	}
}

func TestLoadGraphBackendAcceptsExplicitNornicDB(t *testing.T) {
	t.Parallel()

	got, err := LoadGraphBackend(func(key string) string {
		if key == "ESHU_GRAPH_BACKEND" {
			return "nornicdb"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("LoadGraphBackend() error = %v, want nil", err)
	}
	if got != GraphBackendNornicDB {
		t.Fatalf("LoadGraphBackend() = %q, want %q", got, GraphBackendNornicDB)
	}
}

func TestLoadGraphBackendRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	_, err := LoadGraphBackend(func(key string) string {
		if key == "ESHU_GRAPH_BACKEND" {
			return "not-a-backend"
		}
		return ""
	})
	if err == nil {
		t.Fatal("LoadGraphBackend() error = nil, want non-nil")
	}
}

func TestLoadNeo4jConfigRejectsInvalidPoolTuning(t *testing.T) {
	t.Parallel()

	_, err := LoadNeo4jConfig(func(key string) string {
		switch key {
		case "NEO4J_URI":
			return "bolt://localhost:7687"
		case "NEO4J_USERNAME":
			return "neo4j"
		case "NEO4J_PASSWORD":
			return "change-me"
		case "ESHU_NEO4J_VERIFY_TIMEOUT":
			return "bad-duration"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("LoadNeo4jConfig() error = nil, want non-nil")
	}
}

func TestApplyNeo4jConfigAppliesSharedPoolSettings(t *testing.T) {
	t.Parallel()

	target := &neo4jconfig.Config{}
	cfg := Neo4jConfig{
		MaxConnectionPoolSize:        12,
		MaxConnectionLifetime:        2 * time.Hour,
		ConnectionAcquisitionTimeout: 30 * time.Second,
		SocketConnectTimeout:         7 * time.Second,
	}

	ApplyNeo4jConfig(target, cfg)

	if got, want := target.MaxConnectionPoolSize, 12; got != want {
		t.Fatalf("MaxConnectionPoolSize = %d, want %d", got, want)
	}
	if got, want := target.MaxConnectionLifetime, 2*time.Hour; got != want {
		t.Fatalf("MaxConnectionLifetime = %v, want %v", got, want)
	}
	if got, want := target.ConnectionAcquisitionTimeout, 30*time.Second; got != want {
		t.Fatalf("ConnectionAcquisitionTimeout = %v, want %v", got, want)
	}
	if got, want := target.SocketConnectTimeout, 7*time.Second; got != want {
		t.Fatalf("SocketConnectTimeout = %v, want %v", got, want)
	}
}

type recordingPostgresPoolSetter struct {
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
	connMaxIdleTime time.Duration
}

func (r *recordingPostgresPoolSetter) SetMaxOpenConns(value int) {
	r.maxOpenConns = value
}

func (r *recordingPostgresPoolSetter) SetMaxIdleConns(value int) {
	r.maxIdleConns = value
}

func (r *recordingPostgresPoolSetter) SetConnMaxLifetime(value time.Duration) {
	r.connMaxLifetime = value
}

func (r *recordingPostgresPoolSetter) SetConnMaxIdleTime(value time.Duration) {
	r.connMaxIdleTime = value
}
