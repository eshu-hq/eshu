package query

// openAPIPathsRepositoriesBranches documents the repository branches/refs route.
// It is a separate const from openAPIPathsRepositories to keep that file under
// the source size cap.
const openAPIPathsRepositoriesBranches = `
    "/api/v0/repositories/{repo_id}/branches": {
      "get": {
        "tags": ["repositories"],
        "summary": "Get repository refs",
        "description": "Returns the repository refs the console branch selector uses. Git branch names are not captured by ingestion yet, so this reports the single indexed commit ref per repository (head_sha + last_indexed_at), truth-labeled as derived, rather than a fabricated multi-branch list. When ref ingestion lands this returns the full default_branch and per-branch head list.",
        "operationId": "getRepositoryBranches",
        "parameters": [
          {"$ref": "#/components/parameters/RepoId"}
        ],
        "responses": {
          "200": {
            "description": "Repository refs",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "default_branch": {"type": "string"},
                    "branches": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "name": {"type": "string"},
                          "head_sha": {"type": "string"},
                          "last_indexed_at": {"type": "string"}
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
