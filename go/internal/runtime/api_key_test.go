package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAPIKeyUsesExplicitEnvironmentToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ESHU_HOME", home)
	t.Setenv("ESHU_API_KEY", "env-token")
	t.Setenv("ESHU_AUTO_GENERATE_API_KEY", "")

	got, err := ResolveAPIKey(os.Getenv)
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v, want nil", err)
	}
	if got != "env-token" {
		t.Fatalf("ResolveAPIKey() = %q, want %q", got, "env-token")
	}

	if _, err := os.Stat(filepath.Join(home, ".env")); !os.IsNotExist(err) {
		t.Fatalf("ResolveAPIKey() created config file = %v, want none", err)
	}
}

func TestResolveAPIKeyReusesPersistedToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ESHU_HOME", home)
	t.Setenv("ESHU_API_KEY", "")
	t.Setenv("ESHU_AUTO_GENERATE_API_KEY", "")

	envPath := filepath.Join(home, ".env")
	if err := os.WriteFile(envPath, []byte("OTHER=value\nESHU_API_KEY=persisted-token\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	got, err := ResolveAPIKey(os.Getenv)
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v, want nil", err)
	}
	if got != "persisted-token" {
		t.Fatalf("ResolveAPIKey() = %q, want %q", got, "persisted-token")
	}

	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v, want nil", envPath, err)
	}
	if !strings.Contains(string(envBytes), "ESHU_API_KEY=persisted-token") {
		t.Fatalf(".env = %q, want persisted token", string(envBytes))
	}
}

func TestResolveAPIKeyAutoGeneratesAndPersistsToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ESHU_HOME", home)
	t.Setenv("ESHU_API_KEY", "")
	t.Setenv("ESHU_AUTO_GENERATE_API_KEY", "true")

	got, err := ResolveAPIKey(os.Getenv)
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v, want nil", err)
	}
	if got == "" {
		t.Fatal("ResolveAPIKey() = empty, want generated token")
	}

	envPath := filepath.Join(home, ".env")
	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v, want nil", envPath, err)
	}
	if !strings.Contains(string(envBytes), "ESHU_API_KEY="+got) {
		t.Fatalf(".env = %q, want generated token", string(envBytes))
	}

	again, err := ResolveAPIKey(os.Getenv)
	if err != nil {
		t.Fatalf("ResolveAPIKey() second call error = %v, want nil", err)
	}
	if again != got {
		t.Fatalf("ResolveAPIKey() second call = %q, want %q", again, got)
	}
}

func TestResolveAPIKeyReturnsEmptyWhenAutoGenerationDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ESHU_HOME", home)
	t.Setenv("ESHU_API_KEY", "")
	t.Setenv("ESHU_AUTO_GENERATE_API_KEY", "false")

	got, err := ResolveAPIKey(os.Getenv)
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("ResolveAPIKey() = %q, want empty", got)
	}

	if _, err := os.Stat(filepath.Join(home, ".env")); !os.IsNotExist(err) {
		t.Fatalf("ResolveAPIKey() created config file = %v, want none", err)
	}
}
