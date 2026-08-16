// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

// This package still reads no flags -- go/cmd/eshu resolves every one and
// passes plain values in. It does render flag names, though: several Run
// functions reject empty input with "--<flag> is required", which names a
// flag an operator has to type. Whoever prints the name has to own it, so the
// names live here and go/cmd/eshu registers what this package declares. The
// alternative, a literal on each side, is two owners of one string with
// nothing holding them together: a rename in the wrapper would leave these
// messages pointing at a flag that no longer exists, and no gate would
// notice.

const (
	// InstanceFlag is the collector instance selector on
	// `eshu component enable` and `eshu component disable`. RunEnable and
	// RunDisable render it when the instance ID is empty.
	InstanceFlag = "instance"
	// VersionFlag is the component version selector on
	// `eshu component uninstall`. RunUninstall renders it when the version
	// is empty.
	VersionFlag = "version"
	// InitIDFlag is the component ID input of
	// `eshu component init collector`. RunInitCollector renders it when the
	// ID is empty or malformed.
	InitIDFlag = "id"
	// InitPublisherFlag is the publisher identity input of
	// `eshu component init collector`. RunInitCollector renders it when the
	// publisher is empty or malformed.
	InitPublisherFlag = "publisher"
	// InitFactKindFlag is the emitted fact kind input of
	// `eshu component init collector`. RunInitCollector renders it when the
	// fact kind is empty or malformed.
	InitFactKindFlag = "fact-kind"
)
