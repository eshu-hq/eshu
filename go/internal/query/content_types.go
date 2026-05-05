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
}
