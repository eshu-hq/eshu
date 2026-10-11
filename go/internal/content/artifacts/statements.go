// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"strings"
	"sync"
	"unicode"
)

// ListStatement is the one statement that lists a repository's files. It
// selects an empty string for content, so a list carries no content bytes, and
// orders by relative_path so the first-N windows of the config-artifact load
// are the same on every reader. $1 is the repository id, $2 the row limit.
const ListStatement = `
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       '', content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE repo_id = $1
		ORDER BY relative_path
		LIMIT $2
	`

// GetFileStatement is the one statement that reads one stored file, content
// included. $1 is the repository id, $2 the relative path. It selects the
// columns of ListStatement in the same order, so FileScanTargets maps all three.
const GetFileStatement = `
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       content, content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE repo_id = $1 AND relative_path = $2
	`

// GetFilesStatement is the one statement that reads the stored rows of a set
// of paths. The predicate is `relative_path = ANY($2)`: equality against the
// array, not LIKE, so a path with %, _, a backslash, a quote or a space
// matches only itself. The (repo_id, relative_path) primary key leaves at most
// one row per path, which the content_files_repo_path_pattern_idx index
// serves without a sequential scan. It keeps the column list of
// GetFileStatement so both scan into the same File. $1 is the repository id,
// $2 the text[] of paths.
const GetFilesStatement = `
		SELECT repo_id, relative_path, coalesce(commit_sha, ''),
		       content, content_hash, line_count, coalesce(language, ''),
		       coalesce(artifact_type, '')
		FROM content_files
		WHERE repo_id = $1 AND relative_path = ANY($2::text[])
	`

// NonBlankPathsStatement is the one statement that decides, where the content
// lives, which of a set of paths hold non-blank content, without moving any
// content bytes. $1 is the repository id, $2 the text[] of paths and $3 the
// character set btrim removes: pass SpaceCharacters().
//
// The statement trims with an explicit character set instead of the default
// btrim, which trims only U+0020, and instead of a regular expression class,
// whose whitespace meaning follows the database locale. The set is every rune
// unicode.IsSpace accepts, so content is blank in SQL exactly when
// strings.TrimSpace(content) is empty in Go. A byte-order mark (U+FEFF) and
// U+200B are not in the set, as in Go: a file holding only one is non-blank.
// The set holds multibyte characters, so the statement assumes a UTF8
// database (a SQL_ASCII database trims byte-wise; LATIN1 errors on non-Latin
// content).
const NonBlankPathsStatement = `
		SELECT relative_path
		FROM content_files
		WHERE repo_id = $1
		  AND relative_path = ANY($2::text[])
		  AND btrim(content, $3::text) <> ''
	`

// FileScanTargets returns the destinations, in column order, that map one row
// of ListStatement, GetFileStatement or GetFilesStatement into f. It is the one
// row mapper of the three statements: pass it to Scan, so the caller keeps the
// scanner's error as is (sql.ErrNoRows on a single-row read) and a column added
// to a statement changes one place.
func FileScanTargets(f *File) []any {
	return []any{
		&f.RepoID, &f.RelativePath, &f.CommitSHA,
		&f.Content, &f.ContentHash, &f.LineCount, &f.Language, &f.ArtifactType,
	}
}

// RelativePathScanTargets returns the destination of the one column of
// NonBlankPathsStatement.
func RelativePathScanTargets(relativePath *string) []any {
	return []any{relativePath}
}

var (
	spaceCharactersOnce sync.Once
	spaceCharacters     string
)

// SpaceCharacters returns every rune for which unicode.IsSpace holds, the
// exact set strings.TrimSpace trims. It is derived by enumeration so the SQL
// predicate of NonBlankPathsStatement cannot drift from the Go standard
// library: a Unicode table update in a later Go release changes the set with
// the toolchain.
func SpaceCharacters() string {
	spaceCharactersOnce.Do(func() {
		var builder strings.Builder
		for r := rune(0); r <= unicode.MaxRune; r++ {
			if unicode.IsSpace(r) {
				builder.WriteRune(r)
			}
		}
		spaceCharacters = builder.String()
	})
	return spaceCharacters
}
