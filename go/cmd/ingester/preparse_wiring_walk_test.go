// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// preparseWiringGaps walks a selector config the way the sync sees it and names
// every selector that can force a reconcile (it has a BaselineResolver field)
// but has no Preparse field, and every Preparse field left nil (#7978
// follow-up, ruling a9-143.3). found counts the Preparse fields it checked.
//
// The walk descends into every struct field, slice or array element, map value
// and key, interface, and pointer, exported or not, so a selector container
// under any field name is reached (#8038). A pointer or map reached twice is
// visited once, so a cycle in the config graph ends the walk. Funcs, channels,
// and slices of plain scalars carry no selector and are skipped.
func preparseWiringGaps(selector any) (gaps []string, found int) {
	type visit struct {
		ptr uintptr
		typ reflect.Type
	}
	seen := map[visit]bool{}
	var walk func(v reflect.Value, path string)
	walk = func(v reflect.Value, path string) {
		switch v.Kind() {
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem(), path)
			}
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			key := visit{v.Pointer(), v.Type()}
			if seen[key] {
				return
			}
			seen[key] = true
			walk(v.Elem(), path)
		case reflect.Map:
			if v.IsNil() || scalarKind(v.Type().Elem().Kind()) && scalarKind(v.Type().Key().Kind()) {
				return
			}
			key := visit{v.Pointer(), v.Type()}
			if seen[key] {
				return
			}
			seen[key] = true
			for it := v.MapRange(); it.Next(); {
				walk(it.Key(), fmt.Sprintf("%s[key %v]", path, it.Key()))
				walk(it.Value(), fmt.Sprintf("%s[%v]", path, it.Key()))
			}
		case reflect.Slice, reflect.Array:
			if scalarKind(v.Type().Elem().Kind()) {
				return
			}
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), fmt.Sprintf("%s[%d]", path, i))
			}
		case reflect.Struct:
			name := fmt.Sprintf("%s (%s)", path, v.Type().Name())
			if field := v.FieldByName("Preparse"); field.IsValid() {
				found++
				if field.Kind() == reflect.Func && field.IsNil() {
					gaps = append(gaps, name+".Preparse is nil")
				}
			} else if v.FieldByName("BaselineResolver").IsValid() {
				gaps = append(gaps, name+" can force a reconcile but has no Preparse field")
			}
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i), path+"."+v.Type().Field(i).Name)
			}
		}
	}
	walk(reflect.ValueOf(selector), reflect.TypeOf(selector).String())
	return gaps, found
}

// scalarKind reports whether a kind holds no nested selector: a container of
// only these kinds cannot hide one, so the walk skips it.
func scalarKind(k reflect.Kind) bool {
	switch k {
	case reflect.Bool, reflect.String, reflect.Func, reflect.Chan, reflect.UnsafePointer,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return true
	}
	return false
}

// checkPreparseWiring is the wiring guard every binary's preparse test calls.
// It returns the walk's gaps plus a problem when the number of Preparse-bearing
// selectors the walk checked differs from wantFound, the count pinned for the
// binary. The pin makes a selector the walk missed, or one that quietly
// appeared, fail loudly instead of leaving found above zero (#8038).
func checkPreparseWiring(selector any, wantFound int) []string {
	gaps, found := preparseWiringGaps(selector)
	problems := append([]string(nil), gaps...)
	if found != wantFound {
		problems = append(problems, fmt.Sprintf("checked %d Preparse-bearing selectors, pinned count is %d", found, wantFound))
	}
	return problems
}

// seedUnwired stands in for a selector that can force a reconcile (it has a
// BaselineResolver field) but has no Preparse field.
type seedUnwired struct{ BaselineResolver any }

// seedWired stands in for a selector that carries the Preparse field.
type seedWired struct {
	BaselineResolver any
	Preparse         func()
}

type seedSliceHolder struct {
	Primary  seedWired
	Fallback []any
}

type seedMapHolder struct{ ByTier map[string]any }

type seedIfaceHolder struct{ Inner any }

type seedPrivateHolder struct{ inner *seedUnwired }

type seedCycle struct {
	Self     *seedCycle
	Preparse func()
}

// TestPreparseWalkVisitsEveryContainerShape seeds a selector nested under
// fields that are not named Selectors and expects the walk to reach it.
func TestPreparseWalkVisitsEveryContainerShape(t *testing.T) {
	t.Parallel()
	wired := func() {}
	cases := []struct {
		name      string
		selector  any
		wantFound int
		wantGap   string
	}{
		{"slice field not named Selectors", seedSliceHolder{Primary: seedWired{Preparse: wired}, Fallback: []any{seedUnwired{}}}, 1, "Fallback[0]"},
		{"map field", seedMapHolder{ByTier: map[string]any{"hot": &seedUnwired{}}}, 0, "ByTier[hot]"},
		{"interface field", seedIfaceHolder{Inner: &seedUnwired{}}, 0, "Inner"},
		{"unexported pointer field", seedPrivateHolder{inner: &seedUnwired{}}, 0, "inner"},
		{"wired selector nested in a map", seedMapHolder{ByTier: map[string]any{"hot": seedWired{Preparse: wired}}}, 1, ""},
	}
	for _, tc := range cases {
		gaps, found := preparseWiringGaps(tc.selector)
		if found != tc.wantFound {
			t.Errorf("%s: walk checked %d Preparse-bearing selectors, want %d (gaps %v)", tc.name, found, tc.wantFound, gaps)
		}
		joined := strings.Join(gaps, "\n")
		switch {
		case tc.wantGap == "" && len(gaps) > 0:
			t.Errorf("%s: unexpected gaps %v", tc.name, gaps)
		case tc.wantGap != "" && !strings.Contains(joined, tc.wantGap):
			t.Errorf("%s: gaps %v do not name the nested selector at %q", tc.name, gaps, tc.wantGap)
		}
	}
}

// TestPreparseWalkTerminatesOnCycles proves a pointer cycle in the config
// graph does not hang the walk.
func TestPreparseWalkTerminatesOnCycles(t *testing.T) {
	t.Parallel()
	cycle := &seedCycle{Preparse: func() {}}
	cycle.Self = cycle
	gaps, found := preparseWiringGaps(cycle)
	if found != 1 || len(gaps) > 0 {
		t.Errorf("cycle: found %d gaps %v, want found 1 and no gaps", found, gaps)
	}
}

// TestCheckPreparseWiringFailsOnPinMismatch seeds a clean tree and a pinned
// count that disagrees with it: the guard must name the pin, and must stay
// quiet when the pin matches.
func TestCheckPreparseWiringFailsOnPinMismatch(t *testing.T) {
	t.Parallel()
	clean := seedSliceHolder{Primary: seedWired{Preparse: func() {}}}
	if problems := checkPreparseWiring(clean, 1); len(problems) != 0 {
		t.Errorf("matching pin: unexpected problems %v", problems)
	}
	problems := checkPreparseWiring(clean, 2)
	if len(problems) != 1 || !strings.Contains(problems[0], "pinned count is 2") {
		t.Errorf("mismatched pin: problems %v, want one naming the pinned count 2", problems)
	}
}
