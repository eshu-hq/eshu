// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
)

// unwrapNilAdmissionPort returns nil when port holds a nil dynamic value
// of any nilable kind, so every typed-nil admission-port implementation
// fails closed to 503 instead of panicking on first use. A plain == nil
// check cannot see through a non-nil interface holding a nil pointer, and
// a concrete-type assertion only covers today's adapter, so the check
// inspects the dynamic value itself.
func unwrapNilAdmissionPort[T any](port T) T {
	v := reflect.ValueOf(port)
	if !v.IsValid() {
		return port
	}
	k := v.Kind()
	if (k == reflect.Chan ||
		k == reflect.Func ||
		k == reflect.Interface ||
		k == reflect.Map ||
		k == reflect.Pointer ||
		k == reflect.Slice ||
		k == reflect.UnsafePointer) && v.IsNil() {
		var zero T
		return zero
	}
	return port
}
