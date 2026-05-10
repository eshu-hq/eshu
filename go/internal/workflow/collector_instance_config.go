package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

type desiredCollectorInstanceConfig struct {
	InstanceID    string          `json:"instance_id"`
	CollectorKind string          `json:"collector_kind"`
	Mode          string          `json:"mode"`
	Enabled       bool            `json:"enabled"`
	Bootstrap     bool            `json:"bootstrap"`
	ClaimsEnabled bool            `json:"claims_enabled"`
	DisplayName   string          `json:"display_name"`
	Configuration json.RawMessage `json:"configuration"`
}

// ParseDesiredCollectorInstancesJSON parses operator-supplied collector
// instance JSON into the desired workflow shape shared by coordinators and
// claim-aware collector runtimes.
func ParseDesiredCollectorInstancesJSON(raw string) ([]DesiredCollectorInstance, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	var decoded []desiredCollectorInstanceConfig
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, fmt.Errorf("parse collector instances JSON: %w", err)
	}

	instances := make([]DesiredCollectorInstance, 0, len(decoded))
	for _, candidate := range decoded {
		instance := DesiredCollectorInstance{
			InstanceID:    strings.TrimSpace(candidate.InstanceID),
			CollectorKind: scope.CollectorKind(strings.TrimSpace(candidate.CollectorKind)),
			Mode:          CollectorMode(strings.TrimSpace(candidate.Mode)),
			Enabled:       candidate.Enabled,
			Bootstrap:     candidate.Bootstrap,
			ClaimsEnabled: candidate.ClaimsEnabled,
			DisplayName:   strings.TrimSpace(candidate.DisplayName),
			Configuration: string(candidate.Configuration),
		}
		if strings.TrimSpace(instance.Configuration) == "" {
			instance.Configuration = "{}"
		}
		instances = append(instances, instance)
	}
	return instances, nil
}
