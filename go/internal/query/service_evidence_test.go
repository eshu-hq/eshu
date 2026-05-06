package query

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestLoadServiceQueryEvidenceExtractsHostnamesEnvironmentsAndOpenAPISpecs(t *testing.T) {
	t.Parallel()

	reader := &stubServiceEvidenceReader{
		files: []FileContent{
			{RepoID: "repo-sample-service-api", RelativePath: "deploy/qa/ingress.yaml"},
			{RepoID: "repo-sample-service-api", RelativePath: "deploy/prod/ingress.yaml"},
			{RepoID: "repo-sample-service-api", RelativePath: "specs/index.yaml"},
			{RepoID: "repo-sample-service-api", RelativePath: "src/server.js"},
		},
		fileContent: map[string]string{
			"deploy/qa/ingress.yaml": `
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: sample-service-api
spec:
  rules:
    - host: sample-service-api.qa.example.com
`,
			"deploy/prod/ingress.yaml": `
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: sample-service-api
spec:
  rules:
    - host: sample-service-api.production.example.com
`,
			"specs/index.yaml": `
openapi: 3.0.3
info:
  title: Catalog API
  version: v1
servers:
  - url: https://sample-service-api.qa.example.com
paths:
  /catalog:
    get:
      operationId: listCatalogItems
    post:
      operationId: createCatalogItem
  /catalog/{id}:
    get:
      operationId: getCatalogItem
`,
			"src/server.js": `
const swaggerUi = require('swagger-ui-express')
app.use('/docs', swaggerUi.serve, swaggerUi.setup(spec))
`,
		},
	}

	evidence, err := loadServiceQueryEvidence(context.Background(), reader, "repo-sample-service-api", "sample-service-api")
	if err != nil {
		t.Fatalf("loadServiceQueryEvidence() error = %v, want nil", err)
	}

	gotHostnames := make([]string, 0, len(evidence.Hostnames))
	for _, row := range evidence.Hostnames {
		gotHostnames = append(gotHostnames, row.Hostname)
	}
	wantHostnames := []string{
		"sample-service-api.production.example.com",
		"sample-service-api.qa.example.com",
	}
	if !slices.Equal(gotHostnames, wantHostnames) {
		t.Fatalf("hostnames = %#v, want %#v", gotHostnames, wantHostnames)
	}

	gotEnvironments := make([]string, 0, len(evidence.Environments))
	for _, row := range evidence.Environments {
		gotEnvironments = append(gotEnvironments, row.Environment)
	}
	wantEnvironments := []string{"prod", "qa"}
	if !slices.Equal(gotEnvironments, wantEnvironments) {
		t.Fatalf("environments = %#v, want %#v", gotEnvironments, wantEnvironments)
	}

	if len(evidence.DocsRoutes) != 1 {
		t.Fatalf("len(docs_routes) = %d, want 1", len(evidence.DocsRoutes))
	}
	if got, want := evidence.DocsRoutes[0].Route, "/docs"; got != want {
		t.Fatalf("docs_routes[0].Route = %q, want %q", got, want)
	}

	if len(evidence.APISpecs) != 1 {
		t.Fatalf("len(api_specs) = %d, want 1", len(evidence.APISpecs))
	}
	spec := evidence.APISpecs[0]
	if !spec.Parsed {
		t.Fatal("api_specs[0].Parsed = false, want true")
	}
	if got, want := spec.RelativePath, "specs/index.yaml"; got != want {
		t.Fatalf("api_specs[0].RelativePath = %q, want %q", got, want)
	}
	if got, want := spec.SpecVersion, "3.0.3"; got != want {
		t.Fatalf("api_specs[0].SpecVersion = %q, want %q", got, want)
	}
	if got, want := spec.APIVersion, "v1"; got != want {
		t.Fatalf("api_specs[0].APIVersion = %q, want %q", got, want)
	}
	if got, want := spec.EndpointCount, 2; got != want {
		t.Fatalf("api_specs[0].EndpointCount = %d, want %d", got, want)
	}
	if got, want := spec.MethodCount, 3; got != want {
		t.Fatalf("api_specs[0].MethodCount = %d, want %d", got, want)
	}
	if got, want := spec.OperationIDCount, 3; got != want {
		t.Fatalf("api_specs[0].OperationIDCount = %d, want %d", got, want)
	}
	if got, want := spec.Hostnames, []string{"sample-service-api.qa.example.com"}; !slices.Equal(got, want) {
		t.Fatalf("api_specs[0].Hostnames = %#v, want %#v", got, want)
	}
	if len(spec.Endpoints) != 2 {
		t.Fatalf("len(api_specs[0].Endpoints) = %d, want 2", len(spec.Endpoints))
	}
	if got, want := spec.Endpoints[0].Path, "/catalog"; got != want {
		t.Fatalf("api_specs[0].Endpoints[0].Path = %q, want %q", got, want)
	}
	if got, want := spec.Endpoints[1].OperationIDs, []string{"getCatalogItem"}; !slices.Equal(got, want) {
		t.Fatalf("api_specs[0].Endpoints[1].OperationIDs = %#v, want %#v", got, want)
	}
}

func TestLoadServiceQueryEvidenceMarksJavaScriptSpecFilesAsSpecEvidence(t *testing.T) {
	t.Parallel()

	reader := &stubServiceEvidenceReader{
		files: []FileContent{
			{RepoID: "repo-sample-service-api", RelativePath: "src/spec.js"},
		},
		fileContent: map[string]string{
			"src/spec.js": `
module.exports = {
  openapi: '3.0.0',
}
`,
		},
	}

	evidence, err := loadServiceQueryEvidence(context.Background(), reader, "repo-sample-service-api", "sample-service-api")
	if err != nil {
		t.Fatalf("loadServiceQueryEvidence() error = %v, want nil", err)
	}
	if len(evidence.APISpecs) != 1 {
		t.Fatalf("len(api_specs) = %d, want 1", len(evidence.APISpecs))
	}
	if got, want := evidence.APISpecs[0].RelativePath, "src/spec.js"; got != want {
		t.Fatalf("api_specs[0].RelativePath = %q, want %q", got, want)
	}
	if evidence.APISpecs[0].Parsed {
		t.Fatal("api_specs[0].Parsed = true, want false for javascript scaffold evidence")
	}
	if got, want := evidence.APISpecs[0].Format, "javascript"; got != want {
		t.Fatalf("api_specs[0].Format = %q, want %q", got, want)
	}
}

func TestExtractObservedHostnamesFiltersCodeLikeDottedIdentifiers(t *testing.T) {
	t.Parallel()

	content := `
const server = "console.log"
module.exports = {
  origin: "https://sample-service-api.qa.example.com",
  logger: "module.exports",
}
`

	got := extractObservedHostnames(content)
	want := []string{"sample-service-api.qa.example.com"}
	if !slices.Equal(got, want) {
		t.Fatalf("extractObservedHostnames() = %#v, want %#v", got, want)
	}
}

func TestExtractObservedHostnamesRejectsFileExtensionsAndCamelCase(t *testing.T) {
	t.Parallel()

	content := `
host: "api.qa.example.test"
url: "https://ranking-service.dev.internal.example"
image: "12345.jpg"
logo: "sea-ray-logo.png"
url: "thumbnail.gif"
archive: "scripts.zip"
ref: "logger.debug"
expect: "to.equal"
property: "mediaItem.mediaType"
chain: "externalmediaitem.externalUrl"
stub: "sandbox.stub"
`

	got := extractObservedHostnames(content)
	want := []string{"api.qa.example.test", "ranking-service.dev.internal.example"}
	if !slices.Equal(got, want) {
		t.Fatalf("extractObservedHostnames() = %#v, want %#v", got, want)
	}
}

func TestExtractObservedHostnamesRejectsConfigIdentifierTLDs(t *testing.T) {
	t.Parallel()

	content := `
host: "api.qa.example.test"
host: "db.host"
endpoint: "aws.endpoint"
env: "process.env"
hostname: "config.client.hostname"
`

	got := extractObservedHostnames(content)
	want := []string{"api.qa.example.test"}
	if !slices.Equal(got, want) {
		t.Fatalf("extractObservedHostnames() = %#v, want %#v", got, want)
	}
}

func TestLoadServiceQueryEvidenceResolvesOpenAPIPathsRef(t *testing.T) {
	t.Parallel()

	reader := &stubServiceEvidenceReader{
		files: []FileContent{
			{RepoID: "repo-api", RelativePath: "specs/index.yaml"},
			{RepoID: "repo-api", RelativePath: "api/paths/index.yaml"},
		},
		fileContent: map[string]string{
			"specs/index.yaml": `
openapi: 3.0.3
info:
  title: Catalog API
  version: v1
paths:
  $ref: '../api/paths/index.yaml'
`,
			"api/paths/index.yaml": `
/catalog:
  get:
    operationId: listCatalogItems
  post:
    operationId: createCatalogItem
/catalog/{id}:
  get:
    operationId: getCatalogItem
`,
		},
	}

	evidence, err := loadServiceQueryEvidence(context.Background(), reader, "repo-api", "api")
	if err != nil {
		t.Fatalf("loadServiceQueryEvidence() error = %v", err)
	}

	// Find the parsed spec (the main openapi file)
	var spec *ServiceAPISpecEvidence
	for i := range evidence.APISpecs {
		if evidence.APISpecs[i].Parsed && evidence.APISpecs[i].RelativePath == "specs/index.yaml" {
			spec = &evidence.APISpecs[i]
			break
		}
	}
	if spec == nil {
		t.Fatal("parsed spec for specs/index.yaml not found")
	}
	if got, want := spec.EndpointCount, 2; got != want {
		t.Fatalf("EndpointCount = %d, want %d", got, want)
	}
	if got, want := spec.MethodCount, 3; got != want {
		t.Fatalf("MethodCount = %d, want %d", got, want)
	}
	if got, want := spec.OperationIDCount, 3; got != want {
		t.Fatalf("OperationIDCount = %d, want %d", got, want)
	}
	for _, ep := range spec.Endpoints {
		if ep.Path == "$ref" {
			t.Fatal("$ref must not appear as an endpoint path")
		}
	}
	if got, want := spec.Endpoints[0].Path, "/catalog"; got != want {
		t.Fatalf("Endpoints[0].Path = %q, want %q", got, want)
	}
}

func TestLoadServiceQueryEvidenceResolvesPerPathItemRef(t *testing.T) {
	t.Parallel()

	reader := &stubServiceEvidenceReader{
		files: []FileContent{
			{RepoID: "repo-api", RelativePath: "specs/openapi.yaml"},
			{RepoID: "repo-api", RelativePath: "api/paths/catalog.yaml"},
		},
		fileContent: map[string]string{
			"specs/openapi.yaml": `
openapi: 3.0.3
info:
  title: Catalog API
  version: v2
paths:
  /catalog:
    $ref: '../api/paths/catalog.yaml'
  /health:
    get:
      operationId: healthCheck
`,
			"api/paths/catalog.yaml": `
get:
  operationId: listCatalogItems
post:
  operationId: createCatalogItem
`,
		},
	}

	evidence, err := loadServiceQueryEvidence(context.Background(), reader, "repo-api", "api")
	if err != nil {
		t.Fatalf("loadServiceQueryEvidence() error = %v", err)
	}

	var spec *ServiceAPISpecEvidence
	for i := range evidence.APISpecs {
		if evidence.APISpecs[i].Parsed && evidence.APISpecs[i].RelativePath == "specs/openapi.yaml" {
			spec = &evidence.APISpecs[i]
			break
		}
	}
	if spec == nil {
		t.Fatal("parsed spec for specs/openapi.yaml not found")
	}
	if got, want := spec.EndpointCount, 2; got != want {
		t.Fatalf("EndpointCount = %d, want %d", got, want)
	}
	if got, want := spec.MethodCount, 3; got != want {
		t.Fatalf("MethodCount = %d, want %d", got, want)
	}
	if got, want := spec.OperationIDCount, 3; got != want {
		t.Fatalf("OperationIDCount = %d, want %d", got, want)
	}
	// /catalog should have methods from resolved ref
	for _, ep := range spec.Endpoints {
		if ep.Path == "/catalog" {
			if len(ep.Methods) != 2 {
				t.Fatalf("/catalog methods = %v, want [get post]", ep.Methods)
			}
			return
		}
	}
	t.Fatal("/catalog endpoint not found in resolved spec")
}

func TestLoadServiceQueryEvidenceResolvesNestedOpenAPIPathRefs(t *testing.T) {
	t.Parallel()

	reader := &stubServiceEvidenceReader{
		files: []FileContent{
			{RepoID: "repo-api", RelativePath: "specs/index.yaml"},
			{RepoID: "repo-api", RelativePath: "specs/paths/index.yaml"},
			{RepoID: "repo-api", RelativePath: "specs/paths/widgets.yaml"},
		},
		fileContent: map[string]string{
			"specs/index.yaml": `
openapi: 3.1.0
info:
  title: Service API
  version: v3
paths:
  $ref: './paths/index.yaml'
`,
			"specs/paths/index.yaml": `
/widgets:
  $ref: './widgets.yaml'
`,
			"specs/paths/widgets.yaml": `
get:
  operationId: listWidgets
post:
  operationId: createWidget
`,
		},
	}

	evidence, err := loadServiceQueryEvidence(context.Background(), reader, "repo-api", "api")
	if err != nil {
		t.Fatalf("loadServiceQueryEvidence() error = %v", err)
	}

	var spec *ServiceAPISpecEvidence
	for i := range evidence.APISpecs {
		if evidence.APISpecs[i].Parsed && evidence.APISpecs[i].RelativePath == "specs/index.yaml" {
			spec = &evidence.APISpecs[i]
			break
		}
	}
	if spec == nil {
		t.Fatal("parsed spec for specs/index.yaml not found")
	}
	if got, want := spec.EndpointCount, 1; got != want {
		t.Fatalf("EndpointCount = %d, want %d", got, want)
	}
	if got, want := spec.MethodCount, 2; got != want {
		t.Fatalf("MethodCount = %d, want %d", got, want)
	}
	if got, want := spec.OperationIDCount, 2; got != want {
		t.Fatalf("OperationIDCount = %d, want %d", got, want)
	}
	if got, want := spec.Endpoints[0].Path, "/widgets"; got != want {
		t.Fatalf("Endpoints[0].Path = %q, want %q", got, want)
	}
	if got, want := spec.Endpoints[0].Methods, []string{"get", "post"}; !slices.Equal(got, want) {
		t.Fatalf("Endpoints[0].Methods = %#v, want %#v", got, want)
	}
	if got, want := spec.Endpoints[0].OperationIDs, []string{"createWidget", "listWidgets"}; !slices.Equal(got, want) {
		t.Fatalf("Endpoints[0].OperationIDs = %#v, want %#v", got, want)
	}
}

func TestLoadServiceQueryEvidencePropagatesReaderErrors(t *testing.T) {
	t.Parallel()

	reader := &stubServiceEvidenceReader{
		listErr: errors.New("boom"),
	}

	_, err := loadServiceQueryEvidence(context.Background(), reader, "repo-sample-service-api", "sample-service-api")
	if err == nil {
		t.Fatal("loadServiceQueryEvidence() error = nil, want non-nil")
	}
}

type stubServiceEvidenceReader struct {
	files       []FileContent
	fileContent map[string]string
	listErr     error
	readErr     error
}

func (s *stubServiceEvidenceReader) ListRepoFiles(context.Context, string, int) ([]FileContent, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]FileContent(nil), s.files...), nil
}

func (s *stubServiceEvidenceReader) GetFileContent(_ context.Context, repoID, relativePath string) (*FileContent, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	content, ok := s.fileContent[relativePath]
	if !ok {
		return nil, nil
	}
	return &FileContent{
		RepoID:       repoID,
		RelativePath: relativePath,
		Content:      content,
	}, nil
}
