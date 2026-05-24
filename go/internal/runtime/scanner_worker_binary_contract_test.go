package runtime

import (
	"strings"
	"testing"
)

func TestScannerWorkerBinaryIsBuiltInstalledAndDocumented(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]string{
		"Dockerfile":                                 "-o /go-bin/eshu-scanner-worker ./cmd/scanner-worker",
		"scripts/install-local-binaries.sh":          "go build -trimpath -ldflags=\"$LDFLAGS\" -o \"$INSTALL_DIR/eshu-scanner-worker\" ./cmd/scanner-worker",
		"go/cmd/README.md":                           "`eshu-scanner-worker`",
		"docs/public/deployment/service-runtimes.md": "Scanner Worker",
	} {
		content := readRepositoryFile(t, "../../..", file)
		if !strings.Contains(content, want) {
			t.Fatalf("%s missing %q", file, want)
		}
	}
}
