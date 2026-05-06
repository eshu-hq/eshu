package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	appHomeDirname = ".eshu"
	appHomeEnvVar  = "ESHU_HOME"
	envFileName    = ".env"
)

// appHome returns the Eshu config directory.
func appHome() string {
	if v := os.Getenv(appHomeEnvVar); v != "" {
		if strings.HasPrefix(v, "~") {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, v[1:])
		}
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, appHomeDirname)
}

// envFilePath returns the path to the .env config file.
func envFilePath() string {
	return filepath.Join(appHome(), envFileName)
}

// loadEnvConfig reads the .env file into a map.
func loadEnvConfig() map[string]string {
	config := make(map[string]string)
	f, err := os.Open(envFilePath())
	if err != nil {
		return config
	}
	defer func() {
		_ = f.Close()
	}()

	scanner := bufio.NewScanner(f)
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
	return config
}

// resolveConfigValue resolves a config key, optionally with profile suffix.
func resolveConfigValue(key, profile string) string {
	config := loadEnvConfig()

	// Try profile-specific key first.
	if profile != "" {
		profileKey := key + "_" + strings.ToUpper(profile)
		if v, ok := config[profileKey]; ok && v != "" {
			return v
		}
	}

	// Fall back to base key.
	if v, ok := config[key]; ok {
		return v
	}
	return ""
}

// setConfigValue writes a key=value pair to the .env file.
func setConfigValue(key, value string) error {
	dir := appHome()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	config := loadEnvConfig()
	config[key] = value
	return writeEnvConfig(config)
}

// writeEnvConfig writes the config map to the .env file.
func writeEnvConfig(config map[string]string) error {
	var lines []string
	for k, v := range config {
		lines = append(lines, fmt.Sprintf("%s=%s", k, v))
	}
	// Sort for stable output.
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0 && lines[j] < lines[j-1]; j-- {
			lines[j], lines[j-1] = lines[j-1], lines[j]
		}
	}
	return os.WriteFile(envFilePath(), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
