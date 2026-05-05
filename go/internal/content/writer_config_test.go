package content

import (
	"strings"
	"testing"
)

func TestLoadWriterConfigReadsEntityBatchSize(t *testing.T) {
	t.Parallel()

	cfg, err := LoadWriterConfig(func(key string) string {
		if key == ContentEntityBatchSizeEnv {
			return "600"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("LoadWriterConfig() error = %v, want nil", err)
	}
	if got, want := cfg.EntityBatchSize, 600; got != want {
		t.Fatalf("EntityBatchSize = %d, want %d", got, want)
	}
}

func TestLoadWriterConfigRejectsInvalidEntityBatchSize(t *testing.T) {
	t.Parallel()

	_, err := LoadWriterConfig(func(key string) string {
		if key == ContentEntityBatchSizeEnv {
			return "0"
		}
		return ""
	})
	if err == nil {
		t.Fatal("LoadWriterConfig() error = nil, want error")
	}
	if !strings.Contains(err.Error(), ContentEntityBatchSizeEnv) {
		t.Fatalf("LoadWriterConfig() error = %v, want env name", err)
	}
}

func TestLoadWriterConfigRejectsOversizedEntityBatchSize(t *testing.T) {
	t.Parallel()

	_, err := LoadWriterConfig(func(key string) string {
		if key == ContentEntityBatchSizeEnv {
			return "4001"
		}
		return ""
	})
	if err == nil {
		t.Fatal("LoadWriterConfig() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "4000") {
		t.Fatalf("LoadWriterConfig() error = %v, want max size", err)
	}
}
