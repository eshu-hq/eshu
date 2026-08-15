// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"unicode"

	componentcore "github.com/eshu-hq/eshu/go/internal/component"
)

const componentInitDigestPlaceholder = "0000000000000000000000000000000000000000000000000000000000000000"

var componentInitIdentifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*[a-z0-9]$|^[a-z0-9]$`)

// componentInitCollectorSpec carries every value the scaffold templates
// render. All fields derive deterministically from the four operator inputs,
// so the same flags always produce the same package.
type componentInitCollectorSpec struct {
	ComponentID      string
	Publisher        string
	FactKind         string
	OutputDir        string
	Name             string
	CollectorKind    string
	PackageName      string
	ModulePath       string
	ImageRef         string
	MetricsPrefix    string
	ConsumerPhase    string
	FactConstSuffix  string
	ExampleRecordID  string
	ExampleSourceURI string
	ExampleConfigEnv string
}

// RunInitCollector validates the operator's identifiers, derives the
// scaffold spec, writes a new collector component package under the output
// directory, and reports where it landed. An empty output falls back to
// ./<component-id>; the directory must not already exist.
func RunInitCollector(w io.Writer, jsonOutput bool, id, publisher, factKind, output string) error {
	spec, err := newCollectorSpec(id, publisher, factKind, output)
	if err != nil {
		return renderError(w, jsonOutput, "init", err)
	}
	if err := writeComponentInitCollectorScaffold(spec); err != nil {
		return renderError(w, jsonOutput, "init", err)
	}
	if jsonOutput {
		payload := newCLIOutput("init", "scaffolded")
		componentPayload := CLIComponent{
			ID:        spec.ComponentID,
			Name:      spec.Name,
			Publisher: spec.Publisher,
			Version:   "0.1.0",
		}
		payload.Component = &componentPayload
		return writeJSON(w, payload)
	}
	return writef(w, "scaffolded collector component %s at %s\n", spec.ComponentID, spec.OutputDir)
}

// newCollectorSpec validates the four operator inputs and derives every
// template value. The output directory resolves to an absolute path relative
// to the process working directory, which is the one piece of process state
// this package reads: the scaffold is written where the operator stands.
func newCollectorSpec(id, publisher, factKind, output string) (componentInitCollectorSpec, error) {
	id = strings.TrimSpace(id)
	publisher = strings.TrimSpace(publisher)
	factKind = strings.TrimSpace(factKind)
	output = strings.TrimSpace(output)
	if err := validateComponentInitIdentifier("component id", "id", id); err != nil {
		return componentInitCollectorSpec{}, err
	}
	if err := validateComponentInitIdentifier("publisher", "publisher", publisher); err != nil {
		return componentInitCollectorSpec{}, err
	}
	if err := validateComponentInitIdentifier("fact kind", "fact-kind", factKind); err != nil {
		return componentInitCollectorSpec{}, err
	}
	if !strings.Contains(factKind, ".") {
		return componentInitCollectorSpec{}, componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "fact kind %q must be namespaced", factKind)
	}
	if output == "" {
		output = id
	}
	absoluteOutput, err := filepath.Abs(output)
	if err != nil {
		return componentInitCollectorSpec{}, componentcore.WrapError(componentcore.ErrorCodeInvalidInput, "resolve output directory", err) //nolint:wrapcheck // WrapError carries the component error class the CLI payload needs; %w would lose it
	}
	collectorKind := componentInitLastDottedSegment(id)
	factName := componentInitLastDottedSegment(factKind)
	factSuffix := componentInitGoIdentifierSuffix(factName)
	return componentInitCollectorSpec{
		ComponentID:      id,
		Publisher:        publisher,
		FactKind:         factKind,
		OutputDir:        absoluteOutput,
		Name:             componentInitTitle(factName) + " collector extension",
		CollectorKind:    collectorKind,
		PackageName:      componentInitPackageName(collectorKind),
		ModulePath:       "example.com/" + componentInitPathToken(id),
		ImageRef:         fmt.Sprintf("ghcr.io/%s/%s@sha256:%s", componentInitPathToken(publisher), componentInitPathToken(id), componentInitDigestPlaceholder),
		MetricsPrefix:    "eshu_dp_" + componentInitMetricToken(factKind) + "_",
		ConsumerPhase:    componentInitMetricToken(factKind) + ":provenance_recorded",
		FactConstSuffix:  factSuffix,
		ExampleRecordID:  factName + "-example",
		ExampleSourceURI: "example://" + componentInitPathToken(collectorKind) + "/observations/" + factName + "-example",
		ExampleConfigEnv: "EXAMPLE_" + strings.ToUpper(componentInitMetricToken(collectorKind)) + "_SOURCE",
	}, nil
}

func validateComponentInitIdentifier(field string, flag string, value string) error {
	if strings.TrimSpace(value) == "" {
		return componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "--%s is required", flag)
	}
	if !componentInitIdentifierPattern.MatchString(value) {
		return componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "%s %q must use lowercase letters, numbers, dots, underscores, or hyphens", field, value)
	}
	return nil
}

//nolint:wrapcheck // every branch returns a componentcore error carrying the class the CLI payload needs; %w would lose it
func writeComponentInitCollectorScaffold(spec componentInitCollectorSpec) error {
	if _, err := os.Stat(spec.OutputDir); err == nil {
		return componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "output directory %s already exists", spec.OutputDir)
	} else if !os.IsNotExist(err) {
		return componentcore.WrapError(componentcore.ErrorCodeInvalidInput, "inspect output directory", err)
	}
	if err := os.MkdirAll(filepath.Dir(spec.OutputDir), 0o750); err != nil {
		return componentcore.WrapError(componentcore.ErrorCodeRegistryWriteFailed, "create output parent", err)
	}
	if err := os.Mkdir(spec.OutputDir, 0o750); err != nil {
		return componentcore.WrapError(componentcore.ErrorCodeRegistryWriteFailed, "create output directory", err)
	}
	for _, file := range componentInitCollectorFiles {
		path := filepath.Join(spec.OutputDir, file.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return componentcore.WrapError(componentcore.ErrorCodeRegistryWriteFailed, "create scaffold directory", err)
		}
		if err := writeComponentInitTemplate(path, file.Mode, file.Body, spec); err != nil {
			return err
		}
	}
	return nil
}

//nolint:wrapcheck // every branch returns a componentcore error carrying the class the CLI payload needs; %w would lose it
func writeComponentInitTemplate(path string, mode os.FileMode, body string, spec componentInitCollectorSpec) error {
	parsed, err := template.New(filepath.Base(path)).Parse(body)
	if err != nil {
		return componentcore.WrapError(componentcore.ErrorCodeRegistryWriteFailed, "parse scaffold template", err)
	}
	var rendered strings.Builder
	if err := parsed.Execute(&rendered, spec); err != nil {
		return componentcore.WrapError(componentcore.ErrorCodeRegistryWriteFailed, "render scaffold template", err)
	}
	if err := os.WriteFile(path, []byte(rendered.String()), mode); err != nil {
		return componentcore.WrapError(componentcore.ErrorCodeRegistryWriteFailed, "write scaffold file", err)
	}
	return nil
}

func componentInitLastDottedSegment(value string) string {
	if index := strings.LastIndex(value, "."); index >= 0 && index+1 < len(value) {
		return value[index+1:]
	}
	return value
}

func componentInitGoIdentifierSuffix(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var out strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		out.WriteRune(unicode.ToUpper(runes[0]))
		for _, r := range runes[1:] {
			out.WriteRune(r)
		}
	}
	if out.Len() == 0 {
		return "Observation"
	}
	return out.String()
}

func componentInitPackageName(value string) string {
	token := componentInitPathToken(value)
	token = strings.ReplaceAll(token, "-", "")
	if token == "" || !unicode.IsLetter([]rune(token)[0]) {
		token = "collector" + token
	}
	return token + "collector"
}

func componentInitPathToken(value string) string {
	var out strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(unicode.ToLower(r))
			continue
		}
		if out.Len() > 0 && !strings.HasSuffix(out.String(), "-") {
			out.WriteRune('-')
		}
	}
	return strings.Trim(out.String(), "-")
}

func componentInitMetricToken(value string) string {
	var out strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(unicode.ToLower(r))
			continue
		}
		if out.Len() > 0 && !strings.HasSuffix(out.String(), "_") {
			out.WriteRune('_')
		}
	}
	return strings.Trim(out.String(), "_")
}

func componentInitTitle(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '.' || r == '_' || r == '-'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}
