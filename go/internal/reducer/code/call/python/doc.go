// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package python holds the Python call resolver (declared-base-class method
// resolution, direct and inherited) and Python metaclass edge extraction
// (USES_METACLASS rows). It reads the shared entity index and candidate-name
// helpers from code/call/shared and never imports code/call or its sibling
// language leaves.
package python
