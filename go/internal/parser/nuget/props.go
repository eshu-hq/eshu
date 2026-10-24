// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// Bounds for the MSBuild shared-props files (Directory.Packages.props and
// Directory.Build.props). Every bound is a count or a byte length, never a
// duration, and every excess is counted in the payload's msbuild_props_limits
// bucket instead of being dropped silently.
const (
	// MaxPropsInputBytes is the largest props file the scanner reads. A larger
	// file yields no rows and input_over_limit=true.
	MaxPropsInputBytes = 4 << 20
	// MaxPropsDepth is the deepest XML element nesting the scanner follows. A
	// deeper file yields no rows and depth_exceeded=true.
	MaxPropsDepth = 64
	// MaxPropsItemRows caps PackageVersion, GlobalPackageReference, and
	// PackageReference rows kept from one file.
	MaxPropsItemRows = 5000
	// MaxPropsPropertyRows caps both the distinct property names tracked and the
	// property rows kept from one file.
	MaxPropsPropertyRows = 2000
	// MaxPropsNameBytes caps a package id or property name.
	MaxPropsNameBytes = 256
	// MaxPropsVersionBytes caps a declared or substituted version text.
	MaxPropsVersionBytes = 256
	// MaxPropsValueBytes caps a property value and an asset list.
	MaxPropsValueBytes = 1024
	// MaxPropsConditionBytes caps a Condition expression.
	MaxPropsConditionBytes = 1024
)

var errPropsTooDeep = errors.New("msbuild props element nesting exceeds the scanner depth limit")

// propsLimits counts everything the props scanner declined to turn into a row.
type propsLimits struct {
	inputOverLimit      bool
	depthExceeded       bool
	skippedElements     int
	skippedItems        int
	oversizedFields     int
	droppedItemRows     int
	droppedPropertyRows int
}

func (l propsLimits) asMap() map[string]any {
	return map[string]any{
		"input_over_limit":      l.inputOverLimit,
		"depth_exceeded":        l.depthExceeded,
		"skipped_elements":      l.skippedElements,
		"skipped_items":         l.skippedItems,
		"oversized_fields":      l.oversizedFields,
		"dropped_item_rows":     l.droppedItemRows,
		"dropped_property_rows": l.droppedPropertyRows,
	}
}

// isCentralPropsPath reports whether path names one of the two MSBuild
// shared-props files the registry admits by exact name. The registry matches
// names case-insensitively, so this does too.
func isCentralPropsPath(path string) bool {
	switch strings.ToLower(filepath.Base(path)) {
	case "directory.packages.props", "directory.build.props":
		return true
	}
	return false
}

// parseCentralProps turns one Directory.Packages.props or Directory.Build.props
// into dependency and property rows. It is fail-closed: a construct outside
// the documented grammar yields no row and a counter, never a guessed value.
// Malformed XML returns an error, like the .csproj path.
func parseCentralProps(path string, source []byte, isDependency bool, options shared.Options) (map[string]any, error) {
	payload := shared.BasePayload(path, "nuget_project", isDependency)
	var limits propsLimits
	if options.IndexSource {
		payload["source"] = string(source)
	}
	if len(source) > MaxPropsInputBytes {
		limits.inputOverLimit = true
		payload["msbuild_props_limits"] = limits.asMap()
		return payload, nil
	}

	scan := newPropsScan(source, &limits)
	if err := scan.run(); err != nil {
		if !errors.Is(err, errPropsTooDeep) {
			return nil, fmt.Errorf("parse nuget props file %q: %w", path, err)
		}
		limits.depthExceeded = true
		payload["msbuild_props_limits"] = limits.asMap()
		return payload, nil
	}
	payload["variables"] = buildPropsRows(scan, &limits)
	payload["msbuild_props_limits"] = limits.asMap()
	return payload, nil
}
