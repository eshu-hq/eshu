// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/iacreachability"
	"github.com/eshu-hq/eshu/go/internal/proofofvalue"
)

// loadCorpus reads every relevant file under fixtureRoot and groups it by the
// top-level directory, which is treated as the repository ID. This matches the
// loader used by the iacreachability product-truth test so the analyzer sees
// the same input shape.
func loadCorpus(fixtureRoot string) (map[string][]iacreachability.File, error) {
	filesByRepo := map[string][]iacreachability.File{}
	err := filepath.WalkDir(fixtureRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(fixtureRoot, path)
		if err != nil {
			return err
		}
		repoID, repoRelativePath, ok := strings.Cut(filepath.ToSlash(relative), "/")
		if !ok {
			repoID, repoRelativePath = relative, ""
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		filesByRepo[repoID] = append(filesByRepo[repoID], iacreachability.File{
			RepoID:       repoID,
			RelativePath: filepath.ToSlash(repoRelativePath),
			Content:      string(content),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk corpus %s: %w", fixtureRoot, err)
	}
	if len(filesByRepo) == 0 {
		return nil, fmt.Errorf("no files found under %s", fixtureRoot)
	}
	return filesByRepo, nil
}

// loadGroundTruth reads the curated dead-IaC expected-truth file and returns
// the per-artifact assertions the harness scores against.
func loadGroundTruth(expectedPath string) ([]proofofvalue.GroundTruth, error) {
	content, err := os.ReadFile(expectedPath)
	if err != nil {
		return nil, fmt.Errorf("read ground truth %s: %w", expectedPath, err)
	}
	var expected struct {
		Assertions []proofofvalue.GroundTruth `json:"capability_assertions"`
	}
	if err := json.Unmarshal(content, &expected); err != nil {
		return nil, fmt.Errorf("parse ground truth %s: %w", expectedPath, err)
	}
	if len(expected.Assertions) == 0 {
		return nil, fmt.Errorf("ground truth %s has no assertions", expectedPath)
	}
	return expected.Assertions, nil
}
