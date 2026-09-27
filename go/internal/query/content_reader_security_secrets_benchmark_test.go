// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

// BenchmarkHardcodedSecretReadyReadSnapshotLive measures ready side-table reads
// at the two public page sizes against disposable PostgreSQL. Set
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN and
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run it.
func BenchmarkHardcodedSecretReadyReadSnapshotLive(b *testing.B) {
	ctx, db := openSecretProofDatabase(b)
	if _, err := db.ExecContext(ctx, `
INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, language, indexed_at)
SELECT 'repo-benchmark', 'src/f_' || lpad(i::text, 6, '0') || '.go',
       E'package benchmark\ntoken = "benchmark-secret-value-' || i || E'"\n',
       md5(i::text), 2, 'go', now()
FROM generate_series(1, 250) AS i`); err != nil {
		b.Fatalf("seed content_files: %v", err)
	}
	reader := NewContentReader(db)
	for _, limit := range []int{25, 200} {
		b.Run(fmt.Sprintf("limit_%d", limit), func(b *testing.B) {
			req := codequery.HardcodedSecretInvestigationRequest{RepoID: "repo-benchmark", Limit: limit}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				rows, source, err := reader.InvestigateHardcodedSecretsWithSource(ctx, req)
				if err != nil {
					b.Fatal(err)
				}
				if source != codequery.HardcodedSecretReadSideTable || len(rows) != limit {
					b.Fatalf("read source=%q rows=%d, want side_table/%d", source, len(rows), limit)
				}
			}
		})
	}
}
