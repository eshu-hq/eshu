// Package dockerfile extracts Dockerfile runtime evidence that can stay
// independent from the parent parser dispatch package.
//
// RuntimeMetadata returns typed stage, build-platform, argument, environment,
// port, and label evidence from Dockerfile source text, including quoted
// metadata values, legacy ENV key/value form, and top-of-file Dockerfile
// escape-directive handling. Metadata.Map preserves the parent parser payload
// shape used by query and relationship callers.
package dockerfile
