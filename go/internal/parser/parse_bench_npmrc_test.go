// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"bytes"
	"fmt"
)

// buildNpmrc returns an .npmrc of >= minLOC lines that mixes registry lines
// (with userinfo and a query the parser strips) with the credential, comment,
// and unrelated lines a real file holds. Every fifth line is a registry line.
func buildNpmrc(minLOC int) []byte {
	var b bytes.Buffer
	for line := 0; line < minLOC; line++ {
		switch line % 5 {
		case 0:
			fmt.Fprintf(&b, "@scope%d:registry=https://user:pass@npm-%d.example.com/repo/?x=1#f\n", line, line)
		case 1:
			fmt.Fprintf(&b, "//npm-%d.example.com/:_authToken=NpmrcBenchToken%06d\n", line, line)
		case 2:
			fmt.Fprintf(&b, "# mirror %d\n", line)
		case 3:
			b.WriteString("always-auth=true\n")
		default:
			fmt.Fprintf(&b, "//npm-%d.example.com/:email=ci-%d@example.com\n", line, line)
		}
	}
	return b.Bytes()
}
