// Package dockerfile extracts Dockerfile runtime evidence that can stay
// independent from the parent parser dispatch package.
//
// RuntimeMetadata returns typed stage, argument, environment, port, and label
// evidence from Dockerfile source text. Metadata.Map preserves the parent
// parser payload shape used by query and relationship callers.
package dockerfile
