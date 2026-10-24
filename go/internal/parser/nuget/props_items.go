// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nuget

import (
	"encoding/xml"
	"strings"
)

// itemReason is one reason an emitted item row is not proven as written. A row
// with any reason carries item_ambiguity and partial_evidence.
type itemReason uint16

// The bit order is the alphabetical order of the names in itemReasonNames, so
// item_ambiguity lists its reasons sorted.
const (
	reasonCentralManagement itemReason = 1 << iota
	reasonConditionalMetadata
	reasonDuplicateInclude
	reasonExclude
	reasonRemoved
	reasonUnevaluatedElement
	reasonUnresolvedAssetMetadata
	reasonUnresolvedItemSpec
	reasonUpdated
)

var itemReasonNames = [...]string{
	"central_management_unproven",
	"conditional_metadata",
	"duplicate_include",
	"exclude",
	"removed",
	"unevaluated_element",
	"unresolved_asset_metadata",
	"unresolved_item_spec",
	"updated",
}

func (r itemReason) String() string {
	var parts []string
	for i, name := range itemReasonNames {
		if r&(1<<i) != 0 {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, ",")
}

// maxTrackedItemNames caps the distinct literal Include names remembered for
// duplicate and Update/Remove matching. Past it, the item type is demoted with
// duplicate_include, because uniqueness can no longer be proven.
const maxTrackedItemNames = 2 * MaxPropsItemRows

// itemNameState is what the scanner saw for one item type and package name
// (case-insensitive, as MSBuild matches item specs).
type itemNameState struct {
	count int
	// updatedBefore and removedBefore are watermarks: a kept row whose index
	// is below one was defined before a matching Update or Remove.
	updatedBefore int
	removedBefore int
}

// itemTracker records the item operations that change or hide an item after
// its Include. Memory is bounded by maxTrackedItemNames map entries and a few
// counters per item type, whatever the input.
type itemTracker struct {
	names       map[itemNameKey]itemNameState
	overflow    [len(itemKinds)]bool
	wildUpdated [len(itemKinds)]int
	wildRemoved [len(itemKinds)]int
	unresolved  [len(itemKinds)]bool
	unevaluated bool
}

func newItemTracker() itemTracker {
	return itemTracker{names: make(map[itemNameKey]itemNameState)}
}

func itemKindIndex(kind string) int {
	for i, known := range itemKinds {
		if known == kind {
			return i
		}
	}
	return -1
}

// itemNameKey identifies one package name within one item type. name is
// lowercased, because MSBuild matches item specs case-insensitively.
type itemNameKey struct {
	kind int
	name string
}

func newItemNameKey(kind int, name string) itemNameKey {
	return itemNameKey{kind: kind, name: strings.ToLower(name)}
}

// include records an item that names a package in Include. A literal name is
// counted toward duplicates even when the item itself is skipped, because
// MSBuild still creates it. A list, wildcard, expression, or oversized name
// could match any package, so it demotes every row of the item type.
//
// It returns the lowercased name so the row can be matched again without
// another allocation.
func (t *itemTracker) include(kind, name string) string {
	k := itemKindIndex(kind)
	if !literalPackageName(name) || len(name) > MaxPropsNameBytes {
		t.unresolved[k] = true
		return ""
	}
	key := newItemNameKey(k, name)
	if state, ok := t.names[key]; ok {
		state.count++
		t.names[key] = state
		return key.name
	}
	if len(t.names) >= maxTrackedItemNames {
		t.overflow[k] = true
		return key.name
	}
	t.names[key] = itemNameState{count: 1}
	return key.name
}

// modify records an Update or Remove spec. It applies only to items of the
// same type defined before it (watermark is the kept-row count at that
// point), which is MSBuild's evaluation order. A spec part that is not a
// literal name demotes every earlier row of the type.
func (t *itemTracker) modify(kind, spec string, removed bool, watermark int) {
	k := itemKindIndex(kind)
	for _, part := range strings.Split(spec, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !literalPackageName(part) {
			if removed {
				t.wildRemoved[k] = watermark
			} else {
				t.wildUpdated[k] = watermark
			}
			continue
		}
		key := newItemNameKey(k, part)
		state, ok := t.names[key]
		if !ok {
			continue
		}
		if removed {
			state.removedBefore = watermark
		} else {
			state.updatedBefore = watermark
		}
		t.names[key] = state
	}
}

// reasons returns the demotion reasons for the kept row at index.
func (t *itemTracker) reasons(index int, item itemDecl, centralUnproven bool) itemReason {
	reasons := item.reasons
	k := itemKindIndex(item.kind)
	if state, ok := t.names[itemNameKey{kind: k, name: item.lowerName}]; ok {
		if state.count > 1 {
			reasons |= reasonDuplicateInclude
		}
		if index < state.updatedBefore {
			reasons |= reasonUpdated
		}
		if index < state.removedBefore {
			reasons |= reasonRemoved
		}
	}
	if t.overflow[k] {
		reasons |= reasonDuplicateInclude
	}
	if index < t.wildUpdated[k] {
		reasons |= reasonUpdated
	}
	if index < t.wildRemoved[k] {
		reasons |= reasonRemoved
	}
	if t.unresolved[k] {
		reasons |= reasonUnresolvedItemSpec
	}
	if t.unevaluated {
		reasons |= reasonUnevaluatedElement
	}
	if centralUnproven && item.kind != "PackageReference" {
		reasons |= reasonCentralManagement
	}
	return reasons
}

// centralManagementUnproven reports whether this file sets
// ManagePackageVersionsCentrally to anything but one unconditional literal
// true. An absent property proves nothing either way and leaves rows alone.
func centralManagementUnproven(states map[string]*propState) bool {
	state := states["managepackageversionscentrally"]
	if state == nil {
		return false
	}
	return state.status() != propResolved || !strings.EqualFold(state.value, "true")
}

// itemAttributes are the item attribute names the grammar reads. An attribute
// that matches one only case-insensitively is a conflict: MSBuild may read it
// as the same metadata.
var itemAttributes = []string{
	"Include", "Update", "Remove", "Exclude", "Version", "VersionOverride",
	"PrivateAssets", "IncludeAssets", "ExcludeAssets", "Condition",
}

// hasCaseVariantAttribute reports whether attrs carries an unprefixed
// attribute that matches an itemAttributes name only case-insensitively.
func hasCaseVariantAttribute(attrs []xml.Attr) bool {
	for _, attr := range attrs {
		if attr.Name.Space != "" {
			continue
		}
		for _, known := range itemAttributes {
			if attr.Name.Local != known && strings.EqualFold(attr.Name.Local, known) {
				return true
			}
		}
	}
	return false
}
