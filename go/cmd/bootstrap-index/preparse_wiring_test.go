// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
)

// TestBootstrapIndexSelectorCarriesPreparse (ruling a9-143.3): bootstrap-index
// stamps each stored generation's pre-parse identity, so the first sweep after
// a bootstrap can skip the parse; its selector must carry the configuration.
func TestBootstrapIndexSelectorCarriesPreparse(t *testing.T) {
	t.Parallel()
	deps, err := buildBootstrapCollector(context.Background(), &fakeBootstrapSQLDB{}, func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v", err)
	}
	gaps, found := preparseWiringGaps(deps.source.(*git.GitSource).Selector)
	if found == 0 || len(gaps) > 0 {
		t.Errorf("preparse wiring gaps %v (checked %d selectors)", gaps, found)
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
