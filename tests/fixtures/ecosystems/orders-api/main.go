// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command orders-api is an IP-free fixture service that imports
// github.com/acme/lib-common to exercise cross-repo DEPENDS_ON (rc-3).
package main

import lib "github.com/acme/lib-common"

func main() {
	_ = lib.Identity("ok")
}
