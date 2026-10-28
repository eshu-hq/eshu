# Dockerfile Install Search

`POST /api/v0/content/dockerfile-installs/search` finds source declarations
parsed from Dockerfile `RUN` and URL `ADD`. The MCP tool is
`search_dockerfile_installs`. These are declarations in source, not proof that
an image contains the software.

Send an exact `package` name as it appears in the Dockerfile. Matching is case
sensitive, including URL paths. Optional `repo_id` or `repo_ids` selectors
restrict results to authorized repositories. A scoped token cannot widen its
grant through these selectors. A result contains `entity_id`, `repo_id`,
`relative_path`, `line_number`, `manager`, `category`, `name`, `stage`, and
optional `version` for each persisted `DockerfileInstall` content entity.
The entity ID can be used with the normal entity-content read for source
provenance.
Each declaration also projects as a `DockerfileInstall` graph node contained
by its source file; generation retraction removes stale graph nodes.

`limit` defaults to 10 and caps at 50 **matching declarations**. Results sort
by repository ID, path, line, and entity ID. When `truncated` is true, send
`next_cursor` unchanged as `cursor` to continue. The cursor has all four sort
fields plus a basis binding the package and repository scope; omitted fields or
a cursor from a different query are rejected. A repeated install on the same line has
its own entity ID and result row. Replacing or removing a Dockerfile retracts
its former entity rows on the next content projection.

The query uses the PostgreSQL content-entity name index and the normal
`postgres.query` span with operation `search_dockerfile_install`. It does not
interpret Dockerfile source during a read. Parsing happens at ingestion and
unparsed commands are reported only in the Dockerfile's structured file
metadata. Credential-bearing install option values, URL paths, and sensitive
ARG/ENV expansions are excluded from install entities and reference names
before those facts are emitted.
