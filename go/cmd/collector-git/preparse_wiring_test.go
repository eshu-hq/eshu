// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestCollectorGitSelectorsCarryPreparse (ruling a9-143.3): every selector
// collector-git builds that can force a reconcile carries the pre-parse
// configuration, with and without the webhook handoff.
func TestCollectorGitSelectorsCarryPreparse(t *testing.T) {
	t.Parallel()
	for name, env := range map[string]map[string]string{
		"scheduled only":        {},
		"scheduled and webhook": {"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true"},
	} {
		service, err := buildCollectorService(postgres.SQLDB{}, func(key string) string { return env[key] }, nil, nil, nil)
		if err != nil {
			t.Fatalf("%s: buildCollectorService() error = %v", name, err)
		}
		gaps, found := preparseWiringGaps(service.Source.(*git.GitSource).Selector)
		if found == 0 || len(gaps) > 0 {
			t.Errorf("%s: preparse wiring gaps %v (checked %d selectors)", name, gaps, found)
		}
	}
}

// preparseWiringGaps walks a selector tree the way the sync sees it and names
// every selector that can force a reconcile (it has a BaselineResolver field)
// but has no Preparse field, and every Preparse field left nil (#7978
// follow-up, ruling a9-143.3). found counts the Preparse fields it checked, so
// a test can refuse a vacuous pass.
func preparseWiringGaps(selector any) (gaps []string, found int) {
	var walk func(v reflect.Value, path string)
	walk = func(v reflect.Value, path string) {
		for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return
		}
		name := path + v.Type().Name()
		if children := v.FieldByName("Selectors"); children.IsValid() && children.Kind() == reflect.Slice {
			for i := 0; i < children.Len(); i++ {
				walk(children.Index(i), fmt.Sprintf("%s[%d].", name, i))
			}
			return
		}
		field := v.FieldByName("Preparse")
		if !field.IsValid() {
			if v.FieldByName("BaselineResolver").IsValid() {
				gaps = append(gaps, name+" can force a reconcile but has no Preparse field")
			}
			return
		}
		found++
		if field.IsNil() {
			gaps = append(gaps, name+".Preparse is nil")
		}
	}
	walk(reflect.ValueOf(selector), "")
	return gaps, found
}
