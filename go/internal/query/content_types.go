package query

// FileContent is one file from the content store.
type FileContent struct {
	RepoID       string `json:"repo_id"`
	RelativePath string `json:"relative_path"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	Content      string `json:"content"`
	ContentHash  string `json:"content_hash"`
	LineCount    int    `json:"line_count"`
	Language     string `json:"language,omitempty"`
	ArtifactType string `json:"artifact_type,omitempty"`
	// SearchBackend is set to "hybrid" only on rows reordered by the bounded
	// in-request BM25+vector re-rank; it is empty (and omitted on the wire) when
	// the lexical content-index order was served, so the lexical truth basis
	// stays authoritative.
	SearchBackend string `json:"search_backend,omitempty"`
}
