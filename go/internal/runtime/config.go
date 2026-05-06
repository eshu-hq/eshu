package runtime

import (
	"fmt"
	"os"
	"strings"
)

// Config captures the minimal shared process settings for the Go data-plane
// bootstrap lane.
type Config struct {
	ServiceName string
	Command     string
	ListenAddr  string
	MetricsAddr string
}

// LoadConfig builds a validated runtime config for the named service.
func LoadConfig(serviceName string) (Config, error) {
	cfg := Config{
		ServiceName: strings.TrimSpace(serviceName),
		Command:     strings.TrimSpace(serviceName),
		ListenAddr:  envOrDefault("ESHU_LISTEN_ADDR", "0.0.0.0:8080"),
		MetricsAddr: envOrDefault("ESHU_METRICS_ADDR", "0.0.0.0:9464"),
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Validate checks the config for the small set of invariants required by the
// bootstrap lane.
func (c Config) Validate() error {
	if strings.TrimSpace(c.ServiceName) == "" {
		return fmt.Errorf("service name is required")
	}
	if strings.TrimSpace(c.Command) == "" {
		return fmt.Errorf("command is required")
	}
	if strings.TrimSpace(c.ListenAddr) == "" {
		return fmt.Errorf("listen addr is required")
	}
	if strings.TrimSpace(c.MetricsAddr) == "" {
		return fmt.Errorf("metrics addr is required")
	}

	return nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}

	return fallback
}
