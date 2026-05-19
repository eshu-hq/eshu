package postgres

const graphSchemaApplicationsSchemaSQL = `
CREATE TABLE IF NOT EXISTS graph_schema_applications (
    backend TEXT NOT NULL,
    schema_fingerprint TEXT NOT NULL,
    statement_count INTEGER NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (backend, schema_fingerprint)
);

CREATE INDEX IF NOT EXISTS graph_schema_applications_backend_idx
    ON graph_schema_applications (backend, applied_at DESC);
`

func graphSchemaApplicationsBootstrapDefinition() Definition {
	return Definition{
		Name: "graph_schema_applications",
		Path: "schema/data-plane/postgres/021_graph_schema_applications.sql",
		SQL:  graphSchemaApplicationsSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, graphSchemaApplicationsBootstrapDefinition())
}
