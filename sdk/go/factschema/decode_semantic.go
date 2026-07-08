// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import semanticv1 "github.com/eshu-hq/eshu/sdk/go/factschema/semantic/v1"

// DecodeSemanticDocumentationObservation decodes env.Payload into the latest
// semanticv1.DocumentationObservation struct for
// "semantic.documentation_observation".
func DecodeSemanticDocumentationObservation(env Envelope) (semanticv1.DocumentationObservation, error) {
	return decodeLatestMajor[semanticv1.DocumentationObservation](FactKindSemanticDocumentationObservation, env)
}

// EncodeSemanticDocumentationObservation marshals a semanticv1.DocumentationObservation
// into the map[string]any payload shape an Envelope carries.
func EncodeSemanticDocumentationObservation(observation semanticv1.DocumentationObservation) (map[string]any, error) {
	return encodeDirectPayload(observation)
}

// DecodeSemanticCodeHint decodes env.Payload into the latest semanticv1.CodeHint
// struct for "semantic.code_hint".
func DecodeSemanticCodeHint(env Envelope) (semanticv1.CodeHint, error) {
	return decodeLatestMajor[semanticv1.CodeHint](FactKindSemanticCodeHint, env)
}

// EncodeSemanticCodeHint marshals a semanticv1.CodeHint into the map[string]any
// payload shape an Envelope carries.
func EncodeSemanticCodeHint(hint semanticv1.CodeHint) (map[string]any, error) {
	return encodeDirectPayload(hint)
}
