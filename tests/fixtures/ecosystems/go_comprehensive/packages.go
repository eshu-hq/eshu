package comprehensive

import (
	"fmt"
	"log"
	"os"
	pathpkg "path"
	"strings"
)

// Package-level variables.
var (
	Version   = "1.0.0"
	BuildDate string
	logger    = log.New(os.Stderr, "[comprehensive] ", log.LstdFlags)
)

// Constants.
const (
	MaxRetries = 3
	Timeout    = 30
)

func init() {
	logger.Println("Package initialized")
}

// ResolvePath uses aliased import.
func ResolvePath(base, rel string) string {
	return pathpkg.Join(base, rel)
}

// FormatVersion formats the version string.
func FormatVersion() string {
	parts := strings.Split(Version, ".")
	return fmt.Sprintf("v%s", strings.Join(parts, "."))
}
