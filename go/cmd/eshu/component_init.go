// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/component"
)

const componentInitDigestPlaceholder = "0000000000000000000000000000000000000000000000000000000000000000"

var componentInitIdentifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*[a-z0-9]$|^[a-z0-9]$`)

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

func runComponentInitCollector(cmd *cobra.Command, _ []string) error {
	spec, err := componentInitCollectorSpecFromFlags(cmd)
	if err != nil {
		return renderComponentError(cmd, "init", err)
	}
	if err := writeComponentInitCollectorScaffold(spec); err != nil {
		return renderComponentError(cmd, "init", err)
	}
	if componentJSONEnabled(cmd) {
		payload := newComponentCLIOutput("init", "scaffolded")
		componentPayload := componentCLIComponent{
			ID:        spec.ComponentID,
			Name:      spec.Name,
			Publisher: spec.Publisher,
			Version:   "0.1.0",
		}
		payload.Component = &componentPayload
		return writeComponentJSON(cmd.OutOrStdout(), payload)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "scaffolded collector component %s at %s\n", spec.ComponentID, spec.OutputDir)
	return err
}

func componentInitCollectorSpecFromFlags(cmd *cobra.Command) (componentInitCollectorSpec, error) {
	id, _ := cmd.Flags().GetString(componentInitIDFlag)
	publisher, _ := cmd.Flags().GetString(componentInitPublisherFlag)
	factKind, _ := cmd.Flags().GetString(componentInitFactKindFlag)
	output, _ := cmd.Flags().GetString(componentInitOutputFlag)
	id = strings.TrimSpace(id)
	publisher = strings.TrimSpace(publisher)
	factKind = strings.TrimSpace(factKind)
	output = strings.TrimSpace(output)
	if err := validateComponentInitIdentifier("component id", componentInitIDFlag, id); err != nil {
		return componentInitCollectorSpec{}, err
	}
	if err := validateComponentInitIdentifier("publisher", componentInitPublisherFlag, publisher); err != nil {
		return componentInitCollectorSpec{}, err
	}
	if err := validateComponentInitIdentifier("fact kind", componentInitFactKindFlag, factKind); err != nil {
		return componentInitCollectorSpec{}, err
	}
	if !strings.Contains(factKind, ".") {
		return componentInitCollectorSpec{}, component.Errorf(component.ErrorCodeInvalidInput, "fact kind %q must be namespaced", factKind)
	}
	if output == "" {
		output = id
	}
	absoluteOutput, err := filepath.Abs(output)
	if err != nil {
		return componentInitCollectorSpec{}, component.WrapError(component.ErrorCodeInvalidInput, "resolve output directory", err)
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
		return component.Errorf(component.ErrorCodeInvalidInput, "--%s is required", flag)
	}
	if !componentInitIdentifierPattern.MatchString(value) {
		return component.Errorf(component.ErrorCodeInvalidInput, "%s %q must use lowercase letters, numbers, dots, underscores, or hyphens", field, value)
	}
	return nil
}

func writeComponentInitCollectorScaffold(spec componentInitCollectorSpec) error {
	if _, err := os.Stat(spec.OutputDir); err == nil {
		return component.Errorf(component.ErrorCodeInvalidInput, "output directory %s already exists", spec.OutputDir)
	} else if !os.IsNotExist(err) {
		return component.WrapError(component.ErrorCodeInvalidInput, "inspect output directory", err)
	}
	if err := os.MkdirAll(filepath.Dir(spec.OutputDir), 0o755); err != nil {
		return component.WrapError(component.ErrorCodeRegistryWriteFailed, "create output parent", err)
	}
	if err := os.Mkdir(spec.OutputDir, 0o755); err != nil {
		return component.WrapError(component.ErrorCodeRegistryWriteFailed, "create output directory", err)
	}
	for _, file := range componentInitCollectorFiles {
		path := filepath.Join(spec.OutputDir, file.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return component.WrapError(component.ErrorCodeRegistryWriteFailed, "create scaffold directory", err)
		}
		if err := writeComponentInitTemplate(path, file.Mode, file.Body, spec); err != nil {
			return err
		}
	}
	return nil
}

func writeComponentInitTemplate(path string, mode os.FileMode, body string, spec componentInitCollectorSpec) error {
	parsed, err := template.New(filepath.Base(path)).Parse(body)
	if err != nil {
		return component.WrapError(component.ErrorCodeRegistryWriteFailed, "parse scaffold template", err)
	}
	var rendered strings.Builder
	if err := parsed.Execute(&rendered, spec); err != nil {
		return component.WrapError(component.ErrorCodeRegistryWriteFailed, "render scaffold template", err)
	}
	if err := os.WriteFile(path, []byte(rendered.String()), mode); err != nil {
		return component.WrapError(component.ErrorCodeRegistryWriteFailed, "write scaffold file", err)
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
