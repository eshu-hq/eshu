// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ParserFileFactEnvelope builds the durable "file" fact envelope for one parser
// payload, exactly as the Git collector's streaming fact builder does. It is the
// real parser->envelope emission seam reused by the deterministic replay
// parser-fixture flavor (go/internal/replay/parserfixture), so the recorded
// fixtures capture production envelope shape and provenance rather than a
// re-implementation: SourceRef.SourceURI is the absolute file path, the stable
// fact key is "file:<repoID>:<relativePath>", and SourceRef.SourceRecordID
// equals that key.
//
// parsedFile is the map[string]any returned by parser.Engine.ParsePath; it must
// carry the "path" key the parser sets to the file's absolute path. repoPath is
// the repository root the relative path is computed against. The envelope's
// Payload embeds the parser payload verbatim under "parsed_file_data", so a
// recorder marking that subtree opaque preserves the parser output byte for byte.
func ParserFileFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	parsedFile map[string]any,
	isDependency bool,
) facts.Envelope {
	return fileFactEnvelope(repoPath, repoID, scopeID, generationID, observedAt, parsedFile, isDependency)
}
