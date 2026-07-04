// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"fmt"
	"sort"
)

// KindManifest is the payload-usage manifest entry for one reducer-decoded
// fact kind: which fields its typed struct declares, and which of those
// fields reducer handlers actually read, broken down by source file.
type KindManifest struct {
	// FactKind is the factschema.FactKind* constant IDENTIFIER the decode
	// seam references, e.g. "FactKindAWSResource" — not the wire fact-kind
	// string ("aws_resource"). It is what ParseDecodeSeams reads from the
	// decode function body and what factKindSchemaFile keys on, so a consumer
	// of the generated JSON joins on the constant name, not the wire string.
	FactKind string `json:"fact_kind"`
	// DecodeFunc is the reducer-side decode function name, e.g.
	// "decodeAWSResource".
	DecodeFunc string `json:"decode_func"`
	// StructType is the package-qualified typed struct, e.g. "awsv1.Resource".
	StructType string `json:"struct_type"`
	// DeclaredFields lists every schema-declared field the typed struct
	// carries (excluding the untyped Attributes pass-through), sorted by
	// JSON name.
	DeclaredFields []DeclaredField `json:"declared_fields"`
	// UsedFields lists every declared field at least one reducer handler
	// reads, sorted by JSON name.
	UsedFields []UsedField `json:"used_fields"`
}

// DeclaredField is one field a typed factschema struct declares.
type DeclaredField struct {
	JSONName string `json:"json_name"`
	GoName   string `json:"go_name"`
	Required bool   `json:"required"`
}

// UsedField is one declared field at least one reducer handler reads, with
// the file(s) that read it.
type UsedField struct {
	JSONName string   `json:"json_name"`
	GoName   string   `json:"go_name"`
	Files    []string `json:"files"`
}

// Manifest is the full payload-usage manifest: one KindManifest per
// reducer-decoded fact kind, sorted by FactKind.
type Manifest struct {
	Kinds []KindManifest `json:"kinds"`
}

// Violation is one reverse-break finding: a reducer handler in File reads
// GoFieldName off the FactKind's decoded struct, but that field is absent
// from the EXTERNALLY SUPPLIED declared-field set CheckManifest was given —
// in production, the checked-in JSON Schema's properties
// (LoadDeclaredFieldsFromSchemas). This is the "reducer starts requiring a
// payload field no schema declares" break Contract System v1 §6 enforcement
// gate 2 exists to catch.
//
// The comparison is manifest-used-field vs external-declared-set, not
// vs the struct's own fields: a used field is always a member of the
// struct's fields by construction (BuildManifest only records reads of real
// struct fields), so the violation fires only when the external schema and
// the struct have drifted apart — a hand-edited or stale generated schema
// JSON that dropped a field the struct (and its handlers) still use, or (the
// forward-looking case) a registry-derived declared set narrower than the
// struct.
type Violation struct {
	FactKind    string
	DecodeFunc  string
	File        string
	GoFieldName string
}

// String renders the violation naming the handler, fact kind, and field, per
// issue #4573's "failure output names the specific handler, fact kind, and
// field" acceptance criterion.
func (v Violation) String() string {
	return fmt.Sprintf(
		"handler %s reads field %q on fact kind %q (decoded via %s), which is not in that kind's declared schema fields",
		v.File, v.GoFieldName, v.FactKind, v.DecodeFunc,
	)
}

// BuildManifest joins the discovered decode seams, their struct shapes, and
// the field-usage scan into a deterministic Manifest. A seam whose
// QualifiedStruct has no entry in shapes is skipped (defensive: it means the
// struct dir parse did not cover that package, which ParseStructShapes'
// caller is responsible for wiring correctly — building main.go's own
// coverage test on this is not this function's job).
func BuildManifest(seams []DecodeSeam, shapes map[string]StructShape, usage map[string][]FieldUsage) Manifest {
	var kinds []KindManifest
	for _, seam := range seams {
		shape, ok := shapes[seam.QualifiedStruct()]
		if !ok {
			continue
		}

		declared := make([]DeclaredField, 0, len(shape.Fields))
		for _, f := range shape.Fields {
			declared = append(declared, DeclaredField{JSONName: f.JSONName, GoName: f.GoName, Required: f.Required})
		}

		usedFiles := map[string]map[string]struct{}{} // GoName -> set of files
		for _, u := range usage[seam.FuncName] {
			if usedFiles[u.GoFieldName] == nil {
				usedFiles[u.GoFieldName] = map[string]struct{}{}
			}
			usedFiles[u.GoFieldName][u.File] = struct{}{}
		}

		var used []UsedField
		for _, f := range shape.Fields {
			files, ok := usedFiles[f.GoName]
			if !ok {
				continue
			}
			fileList := make([]string, 0, len(files))
			for file := range files {
				fileList = append(fileList, file)
			}
			sort.Strings(fileList)
			used = append(used, UsedField{JSONName: f.JSONName, GoName: f.GoName, Files: fileList})
		}
		sort.Slice(used, func(i, j int) bool { return used[i].JSONName < used[j].JSONName })

		kinds = append(kinds, KindManifest{
			FactKind:       seam.FactKindConst,
			DecodeFunc:     seam.FuncName,
			StructType:     seam.QualifiedStruct(),
			DeclaredFields: declared,
			UsedFields:     used,
		})
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i].FactKind < kinds[j].FactKind })
	return Manifest{Kinds: kinds}
}

// CheckManifest compares every KindManifest's UsedFields against an
// independently supplied declared-field set (declaredOverride, keyed by
// FactKind then JSON field name) and returns one Violation per used field
// absent from that set. A nil or missing declaredOverride entry for a kind
// falls back to the manifest's own DeclaredFields (which the field is
// definitionally already a member of, so no violation can fire) — this
// lets a caller either run the gate against the manifest's own baked-in
// declaration (a no-op sanity check) or against an externally sourced
// declared-field set such as a JSON Schema's properties list or a registry
// payload_schema ref, which is the actual reverse-break check this command
// exists to run.
func CheckManifest(m Manifest, declaredOverride map[string]map[string]struct{}) []Violation {
	var violations []Violation
	for _, kind := range m.Kinds {
		declared := declaredOverride[kind.FactKind]
		if declared == nil {
			declared = map[string]struct{}{}
			for _, f := range kind.DeclaredFields {
				declared[f.JSONName] = struct{}{}
			}
		}
		for _, used := range kind.UsedFields {
			if _, ok := declared[used.JSONName]; ok {
				continue
			}
			for _, file := range used.Files {
				violations = append(violations, Violation{
					FactKind:    kind.FactKind,
					DecodeFunc:  kind.DecodeFunc,
					File:        file,
					GoFieldName: used.GoName,
				})
			}
		}
	}
	sort.Slice(violations, func(i, j int) bool {
		a, b := violations[i], violations[j]
		if a.FactKind != b.FactKind {
			return a.FactKind < b.FactKind
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.GoFieldName < b.GoFieldName
	})
	return violations
}
