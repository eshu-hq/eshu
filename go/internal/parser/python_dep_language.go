// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/pythondep"
)

// parsePythonRequirements adapts the pythondep package's pip-requirements
// parser into the engine contract. The is_dependency flag is preserved so
// downstream pipelines can distinguish primary repo evidence from vendored
// dependency tree files.
func parsePythonRequirements(path string, isDependency bool) (map[string]any, error) {
	payload, err := pythondep.ParseRequirements(path)
	if err != nil {
		return nil, err
	}
	payload["is_dependency"] = isDependency
	return payload, nil
}

// parsePythonTOML dispatches one Python TOML manifest (pyproject.toml,
// Pipfile, or poetry.lock) to the right pythondep entrypoint based on its
// filename. The registry only routes these three exact names to the
// `python_toml` language, so an unknown filename here means the registry has
// drifted from the dispatch table and the engine surfaces an error rather
// than silently emitting an empty payload.
func parsePythonTOML(path string, isDependency bool) (map[string]any, error) {
	name := strings.ToLower(filepath.Base(path))
	var payload map[string]any
	var err error
	switch name {
	case "pyproject.toml":
		payload, err = pythondep.ParsePyProject(path)
	case "pipfile":
		payload, err = pythondep.ParsePipfile(path)
	case "poetry.lock":
		payload, err = pythondep.ParsePoetryLock(path)
	default:
		return nil, fmt.Errorf("python_toml dispatch received unexpected filename %q; update the registry or this dispatcher to keep them in sync", name)
	}
	if err != nil {
		return nil, err
	}
	payload["is_dependency"] = isDependency
	return payload, nil
}
