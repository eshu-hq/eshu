// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadcore

// StringPointer returns a pointer to its argument. It is the write-direction
// complement of DerefString: payload builders need short-lived addresses for
// optional fact fields without naming a throwaway local at every call site.
func StringPointer(value string) *string {
	return &value
}

// BoolPointer returns a pointer to its argument, complementing DerefBool for
// optional fact fields.
func BoolPointer(value bool) *bool {
	return &value
}

// IntPointer returns a pointer to its argument, complementing DerefInt for
// optional fact fields.
func IntPointer(value int) *int {
	return &value
}
