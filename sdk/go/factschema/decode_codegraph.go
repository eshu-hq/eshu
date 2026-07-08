// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"

	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// DecodeCodegraphFile decodes env.Payload into the latest codegraphv1.File
// struct for the "file" fact kind, dispatching on env.SchemaVersion major per
// Contract System v1 §3.2. Callers (reducer handlers) receive either the
// decoded struct or a classified *DecodeError; they must never substitute a
// zero-value struct on error. The returned struct's ParsedFileData field stays
// an untyped map[string]any pass-through — see codegraphv1's package doc for
// why the inner AST shape is intentionally unmodeled (issue #4750).
func DecodeCodegraphFile(env Envelope) (codegraphv1.File, error) {
	var zero codegraphv1.File
	switch major(env.SchemaVersion) {
	case "1":
		return decodeCodegraphFileV1(env.Payload)
	default:
		return zero, &DecodeError{
			FactKind:       FactKindCodegraphFile,
			Classification: ClassificationInputInvalid,
			Err:            fmt.Errorf("%w: %q", ErrUnsupportedSchemaMajor, env.SchemaVersion),
		}
	}
}

// EncodeCodegraphFile marshals a codegraphv1.File into the map[string]any
// payload shape an Envelope carries. It is the inverse of DecodeCodegraphFile
// for schema-version-1 payloads, used by this module's round-trip tests.
func EncodeCodegraphFile(file codegraphv1.File) (map[string]any, error) {
	return encodeToPayload(file)
}

// DecodeCodegraphRepository decodes env.Payload into the latest
// codegraphv1.Repository struct for the "repository" fact kind. See
// DecodeCodegraphFile for the dispatch and error contract.
func DecodeCodegraphRepository(env Envelope) (codegraphv1.Repository, error) {
	return decodeLatestMajor[codegraphv1.Repository](FactKindCodegraphRepository, env)
}

// EncodeCodegraphRepository marshals a codegraphv1.Repository into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeCodegraphRepository for schema-version-1 payloads.
func EncodeCodegraphRepository(repository codegraphv1.Repository) (map[string]any, error) {
	return encodeToPayload(repository)
}

func decodeCodegraphFileV1(payload map[string]any) (codegraphv1.File, error) {
	repoID, err := requiredPayloadString(FactKindCodegraphFile, payload, "repo_id")
	if err != nil {
		return codegraphv1.File{}, err
	}
	relativePath, err := requiredPayloadString(FactKindCodegraphFile, payload, "relative_path")
	if err != nil {
		return codegraphv1.File{}, err
	}
	parsedFileData, err := requiredPayloadMap(FactKindCodegraphFile, payload, "parsed_file_data")
	if err != nil {
		return codegraphv1.File{}, err
	}

	file := codegraphv1.File{
		RepoID:         repoID,
		RelativePath:   relativePath,
		ParsedFileData: parsedFileData,
	}
	if file.GraphID, err = optionalPayloadString(FactKindCodegraphFile, payload, "graph_id"); err != nil {
		return codegraphv1.File{}, err
	}
	if file.GraphKind, err = optionalPayloadString(FactKindCodegraphFile, payload, "graph_kind"); err != nil {
		return codegraphv1.File{}, err
	}
	if file.IsDependency, err = optionalPayloadBool(FactKindCodegraphFile, payload, "is_dependency"); err != nil {
		return codegraphv1.File{}, err
	}
	if file.Language, err = optionalPayloadString(FactKindCodegraphFile, payload, "language"); err != nil {
		return codegraphv1.File{}, err
	}
	return file, nil
}

func requiredPayloadString(factKind string, payload map[string]any, field string) (string, error) {
	raw, ok := payload[field]
	if !ok || raw == nil {
		return "", missingRequiredPayloadField(factKind, field)
	}
	value, ok := raw.(string)
	if !ok {
		return "", decodePayloadFieldError(factKind, field, fmt.Errorf("want string, got %T", raw))
	}
	return value, nil
}

func requiredPayloadMap(factKind string, payload map[string]any, field string) (map[string]any, error) {
	raw, ok := payload[field]
	if !ok || raw == nil {
		return nil, missingRequiredPayloadField(factKind, field)
	}
	value, ok := raw.(map[string]any)
	if !ok {
		return nil, decodePayloadFieldError(factKind, field, fmt.Errorf("want map[string]any, got %T", raw))
	}
	return value, nil
}

func optionalPayloadString(factKind string, payload map[string]any, field string) (*string, error) {
	raw, ok := payload[field]
	if !ok || raw == nil {
		return nil, nil
	}
	value, ok := raw.(string)
	if !ok {
		return nil, decodePayloadFieldError(factKind, field, fmt.Errorf("want string, got %T", raw))
	}
	return &value, nil
}

func optionalPayloadBool(factKind string, payload map[string]any, field string) (*bool, error) {
	raw, ok := payload[field]
	if !ok || raw == nil {
		return nil, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return nil, decodePayloadFieldError(factKind, field, fmt.Errorf("want bool, got %T", raw))
	}
	return &value, nil
}

func missingRequiredPayloadField(factKind string, field string) *DecodeError {
	return &DecodeError{
		FactKind:       factKind,
		Classification: ClassificationInputInvalid,
		Field:          field,
	}
}

func decodePayloadFieldError(factKind string, field string, err error) *DecodeError {
	return &DecodeError{
		FactKind:       factKind,
		Classification: ClassificationInputInvalid,
		Err:            fmt.Errorf("decode payload: factschema: field %q: %w", field, err),
	}
}
