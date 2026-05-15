package query

import (
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
)

const swaggerUIHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Eshu API - Swagger UI</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function () {
      window.ui = SwaggerUIBundle({
        url: "/api/v0/openapi.json",
        dom_id: "#swagger-ui",
        deepLinking: true
      });
    };
  </script>
</body>
</html>
`

const redocHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Eshu API - ReDoc</title>
</head>
<body>
  <redoc spec-url="/api/v0/openapi.json"></redoc>
  <script src="https://cdn.jsdelivr.net/npm/redoc@2/bundles/redoc.standalone.js"></script>
</body>
</html>
`

// OpenAPISpec returns the OpenAPI 3.0 specification for the Eshu Query API.
func OpenAPISpec() string {
	return strings.Replace(
		openAPISpecPrefix+
			openAPIPathsRepositories+
			openAPIPathsEntities+
			openAPIPathsInvestigations+
			openAPIPathsCode+
			openAPIPathsCodeQuality+
			openAPIPathsCodeSecurity+
			openAPIPathsCodeDeadCodeInvestigation+
			openAPIPathsCodeSymbols+
			openAPIPathsIaC+
			openAPIPathsContent+
			openAPIPathsAdmin+
			openAPIPathsInfrastructure+
			openAPIPathsImpact+
			openAPIPathsEvidence+
			openAPIPathsPackageRegistry+
			openAPIPathsStatusAndCompare+
			openAPIComponents,
		"__ESHU_VERSION__",
		buildinfo.AppVersion(),
		1,
	)
}

// ServeOpenAPI returns an HTTP handler that serves the OpenAPI spec.
func ServeOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(OpenAPISpec()))
}

// ServeSwaggerUI serves a browser UI for exploring the OpenAPI schema.
func ServeSwaggerUI(w http.ResponseWriter, _ *http.Request) {
	serveOpenAPIDocumentationHTML(w, swaggerUIHTML)
}

// ServeReDoc serves a reader-friendly OpenAPI reference page.
func ServeReDoc(w http.ResponseWriter, _ *http.Request) {
	serveOpenAPIDocumentationHTML(w, redocHTML)
}

func serveOpenAPIDocumentationHTML(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}
