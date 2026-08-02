// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

type contextOutput struct {
	Context       string `json:"context"`
	IntegrationID int64  `json:"integration_id"`
}

func runContexts(args []string) error {
	fs := flag.NewFlagSet("contexts", flag.ContinueOnError)
	registry := fs.String("registry", "", "path to ci-gates.v1.yaml registry")
	asJSON := fs.Bool("json", false, "emit context and integration id as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registry == "" {
		return fmt.Errorf("--registry is required")
	}
	reg, err := cigates.Load(*registry)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	if validationErrs := reg.ValidateRequiredStatusChecks(); len(validationErrs) > 0 {
		return fmt.Errorf("invalid required status contract: %v", validationErrs)
	}
	if *asJSON {
		output := make([]contextOutput, 0, len(reg.RequiredStatusChecks))
		for _, check := range reg.RequiredStatusChecks {
			output = append(output, contextOutput{
				Context:       check.Context,
				IntegrationID: check.IntegrationID,
			})
		}
		return json.NewEncoder(os.Stdout).Encode(output)
	}
	for _, check := range reg.RequiredStatusChecks {
		_, _ = fmt.Fprintln(os.Stdout, check.Context)
	}
	return nil
}
