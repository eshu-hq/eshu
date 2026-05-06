package runtime

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	apiKeyEnvVar             = "ESHU_API_KEY"
	autoGenerateAPIKeyEnvVar = "ESHU_AUTO_GENERATE_API_KEY"
	eshuHomeEnvVar           = "ESHU_HOME"
	appHomeDirname           = ".eshu"
	envFileName              = ".env"
	envLockFileName          = ".env.lock"
)

// ResolveAPIKey returns the runtime API token contract for local compose and
// operator deployments.
//
// Resolution order:
//  1. explicit ESHU_API_KEY environment variable
//  2. persisted ESHU_HOME/.env entry
//  3. auto-generated token when ESHU_AUTO_GENERATE_API_KEY is truthy
//
// When a token is persisted or generated, it is written back to the .env file
// so the CLI and follow-on runtimes can reuse the same contract.
func ResolveAPIKey(getenv func(string) string) (string, error) {
	if token := strings.TrimSpace(getenv(apiKeyEnvVar)); token != "" {
		return token, nil
	}

	home := appHomeFromGetenv(getenv)
	if err := os.MkdirAll(home, 0o755); err != nil {
		return "", fmt.Errorf("create Eshu home: %w", err)
	}

	lock, err := acquireLock(filepath.Join(home, envLockFileName))
	if err != nil {
		return "", err
	}
	defer func() {
		_ = lock.Close()
	}()

	envPath := filepath.Join(home, envFileName)
	config, err := loadEnvConfig(envPath)
	if err != nil {
		return "", err
	}

	if token := strings.TrimSpace(config[apiKeyEnvVar]); token != "" {
		return token, nil
	}

	if !isTruthy(getenv(autoGenerateAPIKeyEnvVar)) {
		return "", nil
	}

	token, err := generateAPIKey()
	if err != nil {
		return "", err
	}
	config[apiKeyEnvVar] = token
	if err := writeEnvConfig(envPath, config); err != nil {
		return "", err
	}

	return token, nil
}

func appHomeFromGetenv(getenv func(string) string) string {
	if v := strings.TrimSpace(getenv(eshuHomeEnvVar)); v != "" {
		if strings.HasPrefix(v, "~") {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, v[1:])
		}
		return v
	}

	home, _ := os.UserHomeDir()
	return filepath.Join(home, appHomeDirname)
}

func acquireLock(path string) (*os.File, error) {
	lockFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("lock api key state: %w", err)
	}
	return lockFile, nil
}

func generateAPIKey() (string, error) {
	var seed [32]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	return hex.EncodeToString(seed[:]), nil
}

func isTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func loadEnvConfig(path string) (map[string]string, error) {
	config := make(map[string]string)

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return nil, fmt.Errorf("open env config: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		config[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan env config: %w", err)
	}

	return config, nil
}

func writeEnvConfig(path string, config map[string]string) error {
	var lines []string
	for key, value := range config {
		lines = append(lines, fmt.Sprintf("%s=%s", key, value))
	}
	sort.Strings(lines)

	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp env config: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp env config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp env config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace env config: %w", err)
	}

	return nil
}
