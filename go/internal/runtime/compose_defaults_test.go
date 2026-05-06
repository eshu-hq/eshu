package runtime

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeDocument struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image       string         `yaml:"image"`
	DependsOn   any            `yaml:"depends_on"`
	Environment map[string]any `yaml:"environment"`
}

func TestDefaultComposeUsesNornicDBBackend(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.yaml")
	if _, ok := doc.Services["nornicdb"]; !ok {
		t.Fatal("docker-compose.yaml missing nornicdb service")
	}
	if _, ok := doc.Services["neo4j"]; ok {
		t.Fatal("docker-compose.yaml includes neo4j service; default compose should use NornicDB")
	}

	for _, serviceName := range graphRuntimeServices() {
		service := requireComposeService(t, doc, serviceName)
		assertComposeEnv(t, service, "ESHU_GRAPH_BACKEND", "nornicdb")
		assertComposeEnv(t, service, "DEFAULT_DATABASE", "nornic")
		assertComposeEnv(t, service, "NEO4J_URI", "bolt://nornicdb:7687")
	}
}

func TestNeo4jComposeUsesNeo4jBackend(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.neo4j.yml")
	if _, ok := doc.Services["neo4j"]; !ok {
		t.Fatal("docker-compose.neo4j.yml missing neo4j service")
	}
	if _, ok := doc.Services["nornicdb"]; ok {
		t.Fatal("docker-compose.neo4j.yml includes nornicdb service")
	}

	for _, serviceName := range graphRuntimeServices() {
		service := requireComposeService(t, doc, serviceName)
		assertComposeEnv(t, service, "ESHU_GRAPH_BACKEND", "neo4j")
		assertComposeEnv(t, service, "DEFAULT_DATABASE", "neo4j")
		assertComposeEnv(t, service, "NEO4J_URI", "bolt://neo4j:7687")
	}
}

func TestDefaultComposeFilesDoNotStartTelemetry(t *testing.T) {
	t.Parallel()

	for _, fileName := range []string{"docker-compose.yaml", "docker-compose.neo4j.yml"} {
		doc := readComposeDocument(t, fileName)
		if _, ok := doc.Services["jaeger"]; ok {
			t.Fatalf("%s includes jaeger; telemetry must stay in docker-compose.telemetry.yml", fileName)
		}
		if _, ok := doc.Services["otel-collector"]; ok {
			t.Fatalf("%s includes otel-collector; telemetry must stay in docker-compose.telemetry.yml", fileName)
		}

		for _, serviceName := range telemetryRuntimeServices() {
			service := requireComposeService(t, doc, serviceName)
			assertComposeEnvMissing(t, service, "OTEL_EXPORTER_OTLP_ENDPOINT")
			assertComposeEnvMissing(t, service, "OTEL_TRACES_EXPORTER")
			assertComposeDependencyMissing(t, service, "otel-collector")
			assertComposeDependencyMissing(t, service, "jaeger")
		}
	}
}

func TestTelemetryComposeOverlayDefinesTelemetryStack(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.telemetry.yml")
	if _, ok := doc.Services["jaeger"]; !ok {
		t.Fatal("docker-compose.telemetry.yml missing jaeger service")
	}
	if _, ok := doc.Services["otel-collector"]; !ok {
		t.Fatal("docker-compose.telemetry.yml missing otel-collector service")
	}

	for _, serviceName := range telemetryOverlayServices() {
		service := requireComposeService(t, doc, serviceName)
		assertComposeEnv(t, service, "OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4317")
		assertComposeEnv(t, service, "OTEL_TRACES_EXPORTER", "otlp")
		assertComposeDependency(t, service, "otel-collector")
		assertComposeDependency(t, service, "jaeger")
	}
}

func TestRepositoryAutomationUsesCurrentComposeFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")
	for _, relativePath := range repositoryAutomationFiles(t, root) {
		raw, err := os.ReadFile(filepath.Join(root, relativePath))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		content := string(raw)
		for _, retiredName := range []string{
			"docker-compose.nornicdb.yml",
			"docker-compose.template.yml",
		} {
			if strings.Contains(content, retiredName) {
				t.Fatalf("%s references retired compose file %s", relativePath, retiredName)
			}
		}

		usesNeo4jService := strings.Contains(content, "exec -T neo4j") ||
			strings.Contains(content, "port neo4j") ||
			strings.Contains(content, "up -d postgres neo4j")
		isSharedHelper := strings.HasPrefix(relativePath, filepath.Join("scripts", "lib")+string(os.PathSeparator))
		if usesNeo4jService && !isSharedHelper && !strings.Contains(content, "docker-compose.neo4j.yml") {
			t.Fatalf("%s targets the neo4j Compose service without docker-compose.neo4j.yml", relativePath)
		}
		isExecutableAutomation := filepath.Ext(relativePath) != ".md"
		if isExecutableAutomation &&
			strings.Contains(content, "docker-compose.telemetry.yml") &&
			!strings.Contains(content, "docker-compose.yaml") &&
			!strings.Contains(content, "docker-compose.neo4j.yml") {
			t.Fatalf("%s uses the telemetry overlay without an explicit base compose file", relativePath)
		}
	}
}

func TestRepositoryDocumentationStandardsAreEnforced(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")
	for _, relativePath := range []string{
		"README.md",
		"deploy/README.md",
		"docs/openapi/README.md",
		"go/README.md",
		"go/cmd/README.md",
		"go/internal/README.md",
		"go/internal/collector/README.md",
		"go/internal/facts/README.md",
		"go/internal/mcp/README.md",
		"go/internal/parser/README.md",
		"go/internal/projector/README.md",
		"go/internal/query/README.md",
		"go/internal/reducer/README.md",
		"go/internal/relationships/README.md",
		"go/internal/runtime/README.md",
		"go/internal/status/README.md",
		"go/internal/storage/README.md",
		"go/internal/storage/cypher/README.md",
		"go/internal/storage/neo4j/README.md",
		"go/internal/storage/postgres/README.md",
		"go/internal/telemetry/README.md",
		"scripts/README.md",
	} {
		if _, err := os.Stat(filepath.Join(root, relativePath)); err != nil {
			t.Fatalf("documentation ownership root %s missing: %v", relativePath, err)
		}
	}

	agents := readRepositoryFile(t, root, "AGENTS.md")
	claude := readRepositoryFile(t, root, "CLAUDE.md")
	if agents != claude {
		t.Fatal("AGENTS.md and CLAUDE.md diverged; agent standards must stay in lockstep")
	}
	for _, required := range []string{
		"golangci-lint run ./...",
		"Document every new or touched exported Go type",
		"Keep OpenAPI changes in lockstep",
		"Every Go package directory in `go/` has three files: `doc.go`,",
	} {
		if !strings.Contains(agents, required) {
			t.Fatalf("agent standards missing %q", required)
		}
	}

	workflow := readRepositoryFile(t, root, ".github/workflows/test.yml")
	if !strings.Contains(workflow, "golangci-lint run ./...") {
		t.Fatal(".github/workflows/test.yml must run golangci-lint")
	}

	// Every Go package directory under go/ must carry README.md and AGENTS.md
	// alongside its doc.go. This is the new per-package convention; the
	// eshu-folder-doc-keeper skill plus scripts/verify-doc-claims.sh enforce
	// the content shape, but presence is enforced here so a contributor
	// adding a package cannot skip the docs.
	goDir := filepath.Join(root, "go")
	containerDirs := map[string]struct{}{
		filepath.Join(goDir):                                           {},
		filepath.Join(goDir, "cmd"):                                    {},
		filepath.Join(goDir, "internal"):                               {},
		filepath.Join(goDir, "internal", "storage"):                    {},
		filepath.Join(goDir, "internal", "terraformschema", "schemas"): {},
	}
	err := filepath.WalkDir(goDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		base := d.Name()
		if base == "vendor" || base == "testdata" || base == "bin" {
			return filepath.SkipDir
		}
		if _, isContainer := containerDirs[path]; isContainer {
			return nil
		}
		// A directory is a Go package if it contains at least one
		// non-test .go file. doc.go alone counts — packages that ship
		// only a contract (e.g. a reserved-namespace package) still need
		// README.md and AGENTS.md so contributors landing there get the
		// pipeline-position context.
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return readErr
		}
		hasSource := false
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".go") {
				continue
			}
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			hasSource = true
			break
		}
		if !hasSource {
			return nil
		}
		for _, file := range []string{"README.md", "AGENTS.md", "doc.go"} {
			if _, statErr := os.Stat(filepath.Join(path, file)); statErr != nil {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("Go package %s missing required %s", rel, file)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk go directory: %v", err)
	}
}

func TestHTTPDocsMatchServedOpenAPISurface(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")
	httpDocs := readRepositoryFile(t, root, "docs/docs/reference/http-api.md")
	if !strings.Contains(httpDocs, "/api/v0/openapi.json") {
		t.Fatal("HTTP API docs must document the served OpenAPI JSON endpoint")
	}
	for _, supportedRoute := range []string{"/api/v0/docs", "/api/v0/redoc"} {
		if !strings.Contains(httpDocs, supportedRoute) {
			t.Fatalf("HTTP API docs missing served documentation route %s", supportedRoute)
		}
	}
}

func TestInstallLocalBinariesUsesFirstGOPATHEntry(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")
	fakeBin := t.TempDir()
	firstGoPath := filepath.Join(t.TempDir(), "first")
	secondGoPath := filepath.Join(t.TempDir(), "second")
	fakeGo := filepath.Join(fakeBin, "go")
	fakeGoScript := "#!/usr/bin/env bash\n" +
		"if [[ \"$1\" == \"env\" && \"$2\" == \"GOPATH\" ]]; then\n" +
		"  printf '%s:%s\\n' \"$FAKE_GOPATH_FIRST\" \"$FAKE_GOPATH_SECOND\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf 'unexpected go invocation' >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(fakeGo, []byte(fakeGoScript), 0o755); err != nil {
		t.Fatalf("write fake go command: %v", err)
	}

	cmd := exec.Command("bash", "-c", `source "$SCRIPT"; unset GOBIN; resolve_install_dir`)
	cmd.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SCRIPT="+filepath.Join(root, "scripts", "install-local-binaries.sh"),
		"FAKE_GOPATH_FIRST="+firstGoPath,
		"FAKE_GOPATH_SECOND="+secondGoPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve install dir: %v\n%s", err, output)
	}
	if got, want := strings.TrimSpace(string(output)), filepath.Join(firstGoPath, "bin"); got != want {
		t.Fatalf("install dir = %q, want %q", got, want)
	}
}

func graphRuntimeServices() []string {
	return []string{
		"db-migrate",
		"bootstrap-index",
		"eshu",
		"mcp-server",
		"ingester",
		"resolution-engine",
	}
}

func telemetryRuntimeServices() []string {
	services := append([]string{}, graphRuntimeServices()...)
	return append(services, "workflow-coordinator")
}

func telemetryOverlayServices() []string {
	return []string{
		"bootstrap-index",
		"eshu",
		"mcp-server",
		"ingester",
		"resolution-engine",
		"workflow-coordinator",
	}
}

func repositoryAutomationFiles(t *testing.T, root string) []string {
	t.Helper()

	var paths []string
	for _, dir := range []string{".github", "docs", "scripts"} {
		start := filepath.Join(root, dir)
		err := filepath.WalkDir(start, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if filepath.Base(path) == "site" {
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".md", ".sh", ".yml", ".yaml":
				relativePath, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				paths = append(paths, relativePath)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	return paths
}

func readRepositoryFile(t *testing.T, root, relativePath string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(root, relativePath))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	return string(raw)
}

func readComposeDocument(t *testing.T, name string) composeDocument {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}

	var doc composeDocument
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return doc
}

func requireComposeService(t *testing.T, doc composeDocument, name string) composeService {
	t.Helper()

	service, ok := doc.Services[name]
	if !ok {
		t.Fatalf("compose service %q missing", name)
	}
	return service
}

func assertComposeEnv(t *testing.T, service composeService, key, want string) {
	t.Helper()

	got, ok := service.Environment[key]
	if !ok {
		t.Fatalf("compose env %s missing", key)
	}
	if got != want {
		t.Fatalf("compose env %s = %#v, want %q", key, got, want)
	}
}

func assertComposeEnvMissing(t *testing.T, service composeService, key string) {
	t.Helper()

	if _, ok := service.Environment[key]; ok {
		t.Fatalf("compose env %s is set", key)
	}
}

func assertComposeDependency(t *testing.T, service composeService, key string) {
	t.Helper()

	if !composeDependencyExists(service.DependsOn, key) {
		t.Fatalf("compose dependency %s missing", key)
	}
}

func assertComposeDependencyMissing(t *testing.T, service composeService, key string) {
	t.Helper()

	if composeDependencyExists(service.DependsOn, key) {
		t.Fatalf("compose dependency %s is set", key)
	}
}

func composeDependencyExists(dependsOn any, key string) bool {
	switch dependencies := dependsOn.(type) {
	case map[string]any:
		_, ok := dependencies[key]
		return ok
	case []any:
		for _, dependency := range dependencies {
			if dependency == key {
				return true
			}
		}
	}
	return false
}
