// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Layer names how deep in the stack a gate tests: the question a failure of
// the gate answers. It is orthogonal to Category, which names the kind of
// check (exactness, hygiene, race, ...) and drives the CLI's --category
// filter. Layer is descriptive only: selection, blocking and check identity
// never read it.
type Layer string

const (
	// LayerHygiene gates ask whether the change is well-formed.
	LayerHygiene Layer = "hygiene"
	// LayerUnit gates ask whether the code does what its tests say.
	LayerUnit Layer = "unit"
	// LayerContract gates ask whether declared or generated artifacts match the code.
	LayerContract Layer = "contract"
	// LayerReplay gates ask whether recorded inputs through real components
	// give the same result every time.
	LayerReplay Layer = "replay"
	// LayerTruth gates ask whether the whole system produces the known right answer.
	LayerTruth Layer = "truth"
	// LayerPerformance gates ask whether budgets hold on the supported backend.
	LayerPerformance Layer = "performance"
	// LayerSecondary gates are watched but never block a merge.
	LayerSecondary Layer = "secondary"
)

// MaxPurposeLength caps a gate's purpose sentence, in characters (runes, so
// Ifá and Odù count as three letters each), so the generated reference stays
// scannable. A purpose that needs more belongs in the gate's own doc.
const MaxPurposeLength = 160

// layerOrder lists the layers from the cheapest, most local checks to the
// watch-only ones. The generated reference renders layers in this order.
var layerOrder = []Layer{
	LayerHygiene,
	LayerUnit,
	LayerContract,
	LayerReplay,
	LayerTruth,
	LayerPerformance,
	LayerSecondary,
}

// layerQuestions is the one-line question each layer answers.
var layerQuestions = map[Layer]string{
	LayerHygiene:     "Is the change well-formed?",
	LayerUnit:        "Does the code do what its tests say?",
	LayerContract:    "Do declared or generated artifacts match the code?",
	LayerReplay:      "Do recorded inputs through real components give the same result every time?",
	LayerTruth:       "Does the whole system produce the known right answer?",
	LayerPerformance: "Do performance budgets hold on the supported backend?",
	LayerSecondary:   "What do we watch without blocking a merge?",
}

// Layers returns every layer in reference order.
func Layers() []Layer {
	return append([]Layer(nil), layerOrder...)
}

// LayerQuestion returns the question the layer answers, or "" for an unknown layer.
func LayerQuestion(layer Layer) string {
	return layerQuestions[layer]
}

// parseLayer validates a gate's raw layer value. An empty value is allowed
// at load time so test registries need not carry descriptive fields;
// DescriptionCheck requires it on the real registry.
func parseLayer(path, id, raw string) (Layer, error) {
	layer := Layer(strings.TrimSpace(raw))
	if layer == "" {
		return "", nil
	}
	if _, ok := layerQuestions[layer]; !ok {
		names := make([]string, 0, len(layerOrder))
		for _, known := range layerOrder {
			names = append(names, string(known))
		}
		return "", fmt.Errorf("ci-gates registry %s: gate %q has invalid layer %q (valid: %s)",
			path, id, raw, strings.Join(names, ", "))
	}
	return layer, nil
}

// DescriptionCheck reports every gate that is missing the descriptive fields
// a reader needs: a layer, and a one-line purpose of at most
// MaxPurposeLength characters. It also rejects a blocking gate in the
// secondary layer, because secondary means watch-only. `ci-gates validate`
// runs it on the real registry.
func DescriptionCheck(reg *Registry) []error {
	var errs []error
	for _, gate := range reg.Gates {
		if gate.Layer == "" {
			errs = append(errs, fmt.Errorf("gate %q has no layer (valid: %v)", gate.ID, layerOrder))
		}
		switch {
		case gate.Purpose == "":
			errs = append(errs, fmt.Errorf("gate %q has no purpose: add one plain sentence saying what it checks", gate.ID))
		case strings.ContainsAny(gate.Purpose, "\n\r"):
			errs = append(errs, fmt.Errorf("gate %q purpose must be one line", gate.ID))
		case utf8.RuneCountInString(gate.Purpose) > MaxPurposeLength:
			errs = append(errs, fmt.Errorf("gate %q purpose is %d characters; keep it to %d",
				gate.ID, utf8.RuneCountInString(gate.Purpose), MaxPurposeLength))
		}
		if gate.Layer == LayerSecondary && gate.Blocking {
			errs = append(errs, fmt.Errorf("gate %q is in layer secondary but blocking; secondary gates never block", gate.ID))
		}
	}
	return errs
}
