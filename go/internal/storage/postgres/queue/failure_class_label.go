// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queuestore

import "regexp"

// failureClassLabelPattern bounds a failure_class metric label. Every class the
// queues store today is a lowercase constant, but a class can also come from a
// self-classifying cause, and a label must never take an unbounded value.
var failureClassLabelPattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// FailureClassOtherLabel is the label BoundedFailureClassLabel returns for a
// class outside the pattern.
const FailureClassOtherLabel = "other"

// BoundedFailureClassLabel returns class when it is a lowercase identifier of at
// most 64 characters and FailureClassOtherLabel otherwise. It bounds only the
// metric label (#7386); the stored failure_class is not changed.
func BoundedFailureClassLabel(class string) string {
	if failureClassLabelPattern.MatchString(class) {
		return class
	}
	return FailureClassOtherLabel
}
