// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNornicDBTuningDocCanonicalDefaultsMatchCode(t *testing.T) {
	t.Parallel()

	doc := readNornicDBTuningDoc(t)
	cases := map[string]string{
		nornicDBPhaseGroupStatementsEnv:            fmt.Sprint(defaultNornicDBPhaseGroupStatements),
		nornicDBFilePhaseGroupStatementsEnv:        fmt.Sprint(defaultNornicDBFilePhaseStatements),
		nornicDBFileBatchSizeEnv:                   fmt.Sprint(defaultNornicDBFileBatchSize),
		nornicDBEntityPhaseStatementsEnv:           fmt.Sprint(defaultNornicDBEntityPhaseStatements),
		nornicDBEntityBatchSizeEnv:                 fmt.Sprint(defaultNornicDBEntityBatchSize),
		nornicDBEntityLabelBatchSizesEnv:           formatLabelSizes(defaultNornicDBEntityLabelBatchSizes(0)),
		nornicDBEntityLabelPhaseGroupStatementsEnv: formatLabelSizes(defaultNornicDBEntityLabelPhaseGroupStatements(0)),
		nornicDBCanonicalGroupedWritesEnv:          "unset / false",
		nornicDBBatchedEntityContainmentEnv:        "unset / true",
		canonicalWriteTimeoutEnv:                   fmt.Sprintf("%s on NornicDB", defaultNornicDBCanonicalWriteTimeout),
	}
	for envName, wantDefault := range cases {
		envName, wantDefault := envName, wantDefault
		t.Run(envName, func(t *testing.T) {
			t.Parallel()

			gotDefault, ok := markdownTableDefault(doc, envName)
			if !ok {
				t.Fatalf("nornicdb tuning doc missing %s", envName)
			}
			if gotDefault != wantDefault {
				t.Fatalf("doc default for %s = %q, want %q", envName, gotDefault, wantDefault)
			}
		})
	}
}

func readNornicDBTuningDoc(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	docPath := filepath.Join(filepath.Dir(filename), "..", "..", "..", "docs", "public", "reference", "nornicdb-tuning.md")
	contents, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read nornicdb tuning doc: %v", err)
	}
	return string(contents)
}

func markdownTableDefault(markdown string, envName string) (string, bool) {
	prefix := "| `" + envName + "` |"
	for _, line := range strings.Split(markdown, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			return "", false
		}
		return normalizeMarkdownDefault(cells[2]), true
	}
	return "", false
}

func normalizeMarkdownDefault(defaultCell string) string {
	return strings.ReplaceAll(strings.TrimSpace(defaultCell), "`", "")
}

func formatLabelSizes(labelSizes map[string]int) string {
	var builder strings.Builder
	for i, label := range orderedEntityBatchLabels(labelSizes) {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(label)
		builder.WriteByte('=')
		fmt.Fprint(&builder, labelSizes[label])
	}
	return builder.String()
}
