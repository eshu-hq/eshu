// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"strings"
)

// RenderLayerIndex renders the "Gates by layer" section of the generated CI
// gates reference: one subsection per non-empty layer, in reference order,
// each headed by the question the layer answers and listing every gate's id,
// blocking status and purpose in registry order. Gates with no layer render
// last under "Unlabeled" so a missing label is visible rather than dropped.
func RenderLayerIndex(reg *Registry) string {
	byLayer := make(map[Layer][]Gate, len(layerOrder))
	var unlabeled []Gate
	for _, gate := range reg.Gates {
		if gate.Layer == "" {
			unlabeled = append(unlabeled, gate)
			continue
		}
		byLayer[gate.Layer] = append(byLayer[gate.Layer], gate)
	}

	var out strings.Builder
	out.WriteString("## Gates by layer\n\n")
	out.WriteString("Every gate belongs to one layer. A layer names the question a failing gate\n")
	out.WriteString("answers, from the cheapest local checks down to the watch-only ones. The\n")
	out.WriteString("registry's `category` field still records what kind of check a gate is.\n")
	for _, layer := range layerOrder {
		gates := byLayer[layer]
		if len(gates) == 0 {
			continue
		}
		heading := strings.ToUpper(string(layer[:1])) + string(layer[1:])
		fmt.Fprintf(&out, "\n### %s: %s (%s)\n\n", heading, layerQuestions[layer], gateCount(len(gates)))
		writeLayerList(&out, gates)
	}
	if len(unlabeled) > 0 {
		fmt.Fprintf(&out, "\n### Unlabeled (%s)\n\n", gateCount(len(unlabeled)))
		writeLayerList(&out, unlabeled)
	}
	return out.String()
}

// writeLayerList writes one bullet per gate. Bullets, not table rows: the
// generator's self-test counts every "| `" line as a registry row of the main
// gate table, so the layer index must not add rows of that shape.
func writeLayerList(out *strings.Builder, gates []Gate) {
	for _, gate := range gates {
		status := "advisory"
		if gate.Blocking {
			status = "blocking"
		}
		fmt.Fprintf(out, "- `%s` (%s): %s\n", gate.ID, status, gate.Purpose)
	}
}

func gateCount(n int) string {
	if n == 1 {
		return "1 gate"
	}
	return fmt.Sprintf("%d gates", n)
}
