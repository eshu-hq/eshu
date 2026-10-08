// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"reflect"
	"testing"
)

// TestDeadCodeFrameworksWithoutRootModelTable pins the modeled-framework
// census behind deadCodeModeledFrameworks: go models net_http roots
// (registrations plus go.net_http_handler_signature, which also covers chi
// handlers) while gin/echo/fiber have no root model; groovy models jenkins;
// python models fastapi/flask decorator roots while django/drf/aiohttp/
// tornado have none; php keeps its five route/hook entries. A wrong
// "modeled" entry would suppress a true notice.
func TestDeadCodeFrameworksWithoutRootModelTable(t *testing.T) {
	t.Parallel()

	results := []map[string]any{
		{"language": "go", "metadata": map[string]any{"framework": "gin"}},
		{"language": "go", "metadata": map[string]any{"framework": "echo"}},
		{"language": "go", "metadata": map[string]any{"framework": "fiber"}},
		{"language": "go", "metadata": map[string]any{"framework": "net_http"}},
		{"language": "go", "metadata": map[string]any{"framework": "chi"}},
		{"language": "groovy", "metadata": map[string]any{"framework": "jenkins"}},
		{"language": "groovy", "metadata": map[string]any{"framework": "gradle"}},
		{"language": "python", "metadata": map[string]any{"framework": "django"}},
		{"language": "python", "metadata": map[string]any{"framework": "drf"}},
		{"language": "python", "metadata": map[string]any{"framework": "aiohttp"}},
		{"language": "python", "metadata": map[string]any{"framework": "tornado"}},
		{"language": "python", "metadata": map[string]any{"framework": "fastapi"}},
		{"language": "python", "metadata": map[string]any{"framework": "flask"}},
		{"language": "php", "metadata": map[string]any{"framework": "laravel"}},
		{"language": "php", "metadata": map[string]any{"framework": "slim"}},
		{"language": "php", "metadata": map[string]any{"framework": "symfony"}},
		{"language": "php", "metadata": map[string]any{"framework": "wordpress"}},
		{"language": "php", "metadata": map[string]any{"framework": "zend_framework_1"}},
		{"language": "php", "metadata": map[string]any{"framework": "cakephp"}},
		{"language": "ruby", "metadata": map[string]any{"framework": "rails"}},
	}

	got := deadCodeFrameworksWithoutRootModel(results)
	want := map[string][]string{
		"go":     {"echo", "fiber", "gin"},
		"groovy": {"gradle"},
		"python": {"aiohttp", "django", "drf", "tornado"},
		"php":    {"cakephp"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deadCodeFrameworksWithoutRootModel() = %#v, want %#v", got, want)
	}
}
