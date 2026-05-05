package buildinfo

import (
	"fmt"
	"io"
	"strings"
)

// PrintVersionFlag writes the application version for direct service binary
// version probes. It returns handled=false for every argument shape except a
// single --version or -v flag so callers can continue normal startup.
func PrintVersionFlag(args []string, stdout io.Writer, applicationName string) (bool, error) {
	if len(args) != 1 {
		return false, nil
	}
	switch args[0] {
	case "--version", "-v":
	default:
		return false, nil
	}

	name := strings.TrimSpace(applicationName)
	if name == "" {
		name = "eshu"
	}
	_, err := fmt.Fprintf(stdout, "%s %s\n", name, AppVersion())
	return true, err
}
