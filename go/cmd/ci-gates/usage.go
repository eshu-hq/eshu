// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"io"
)

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "usage: ci-gates <select|run|await|contexts|validate|uncovered> [flags]")
	_, _ = fmt.Fprintln(w, "  select    --registry <path> --tier <tier> [--base <ref>] [--paths-from <file|->] [--category <list>] [--explain] [--json]")
	_, _ = fmt.Fprintln(w, "  run       --registry <path> --tier <tier> [--base <ref>] [--paths-from <file|->] [--category <list>] [--repo-root <path>]")
	_, _ = fmt.Fprintln(w, "  await     --registry <path> --repo-root <path> --repo <owner/name> --pr <number> --head-sha <sha> [--poll-interval 30s] [--timeout 55m]")
	_, _ = fmt.Fprintln(w, "  contexts  --registry <path> [--json]")
	_, _ = fmt.Fprintln(w, "  validate  --registry <path> --repo-root <path> [--drift]")
	_, _ = fmt.Fprintln(w, "  uncovered --registry <path> --category <list> --tier <tier> [--base <ref>] [--paths-from <file|->]")
}
