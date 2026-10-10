// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"flag"
	"fmt"
	"reflect"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// runCompare validates two registries and checks their complete public model.
// Fragment paths are deliberately excluded: one input may be a flat view.
func runCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	leftPath := fs.String("left", "", "first ci-gates registry")
	rightPath := fs.String("right", "", "second ci-gates registry")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *leftPath == "" || *rightPath == "" || fs.NArg() != 0 {
		return fmt.Errorf("--left and --right are required; positional arguments are not accepted")
	}
	left, err := cigates.Load(*leftPath)
	if err != nil {
		return fmt.Errorf("load left registry: %w", err)
	}
	right, err := cigates.Load(*rightPath)
	if err != nil {
		return fmt.Errorf("load right registry: %w", err)
	}
	if left.Version != right.Version || !reflect.DeepEqual(left.Gates, right.Gates) ||
		!reflect.DeepEqual(left.RequiredStatusChecks, right.RequiredStatusChecks) ||
		!reflect.DeepEqual(left.HygieneHooks, right.HygieneHooks) ||
		!reflect.DeepEqual(left.NonGateWorkflows, right.NonGateWorkflows) {
		return fmt.Errorf("normalized ci-gates registries differ")
	}
	return nil
}
