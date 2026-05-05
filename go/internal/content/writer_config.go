package content

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// ContentEntityBatchSizeEnv tunes Postgres content-entity upsert statement width.
	ContentEntityBatchSizeEnv = "ESHU_CONTENT_ENTITY_BATCH_SIZE"
	// MaxContentEntityBatchSize keeps the current Postgres writer below the bind-parameter limit.
	MaxContentEntityBatchSize = 4000
)

// WriterConfig contains runtime tunables for source-local content persistence.
type WriterConfig struct {
	EntityBatchSize int
}

// LoadWriterConfig reads content-writer tuning from the process environment.
func LoadWriterConfig(getenv func(string) string) (WriterConfig, error) {
	if getenv == nil {
		return WriterConfig{}, nil
	}

	raw := strings.TrimSpace(getenv(ContentEntityBatchSizeEnv))
	if raw == "" {
		return WriterConfig{}, nil
	}
	size, err := strconv.Atoi(raw)
	if err != nil || size <= 0 {
		return WriterConfig{}, fmt.Errorf("parse %s=%q: must be a positive integer", ContentEntityBatchSizeEnv, raw)
	}
	if size > MaxContentEntityBatchSize {
		return WriterConfig{}, fmt.Errorf(
			"parse %s=%q: must be <= %d",
			ContentEntityBatchSizeEnv,
			raw,
			MaxContentEntityBatchSize,
		)
	}
	return WriterConfig{EntityBatchSize: size}, nil
}
