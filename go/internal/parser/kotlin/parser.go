package kotlin

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// Parse extracts Kotlin declarations, imports, variables, calls, and
// receiver-type metadata from one source file.
func Parse(repoRoot string, path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	packageName := kotlinFilePackage(string(source))

	payload := shared.BasePayload(path, "kotlin", isDependency)
	payload["interfaces"] = []map[string]any{}

	siblingFunctionReturnTypes, err := kotlinCollectSiblingFunctionReturnTypes(repoRoot, path, packageName)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(source), "\n")
	braceDepth := 0
	stack := make([]scopedContext, 0)
	smartCastScopes := make([]kotlinTypeFlowScope, 0)
	whenSubjectScopes := make([]kotlinWhenSubjectScope, 0)
	classTypeParameters := make(map[string][]string)
	interfaceMethods := make(map[string]map[string]struct{})
	classInterfaces := make(map[string]map[string]struct{})
	seenVariables := make(map[string]struct{})
	localVariableTypes := make(map[string]map[string]string)
	localVariableCallKinds := make(map[string]map[string]string)
	classPropertyTypes := make(map[string]map[string]string)
	functionReturnTypes := make(map[string]string, len(siblingFunctionReturnTypes))
	for key, returnType := range siblingFunctionReturnTypes {
		functionReturnTypes[key] = returnType
	}
	knownTypeNames := make(map[string]struct{})
	pendingAnnotations := make([]string, 0)

	for index, rawLine := range lines {
		lineNumber := index + 1
		smartCastScopes = popKotlinTypeFlowScopes(smartCastScopes, braceDepth)
		whenSubjectScopes = popKotlinWhenSubjectScopes(whenSubjectScopes, braceDepth)
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			braceDepth += braceDelta(rawLine)
			stack = popCompletedScopes(stack, braceDepth)
			continue
		}
		lineAnnotations := kotlinAnnotations(trimmed)
		annotations := append(append([]string(nil), pendingAnnotations...), lineAnnotations...)
		if len(lineAnnotations) > 0 &&
			!strings.Contains(trimmed, "fun ") &&
			!kotlinClassPattern.MatchString(trimmed) &&
			!kotlinObjectPattern.MatchString(trimmed) &&
			!kotlinInterfacePattern.MatchString(trimmed) &&
			!kotlinEnumPattern.MatchString(trimmed) {
			pendingAnnotations = append(pendingAnnotations, lineAnnotations...)
			braceDepth += braceDelta(rawLine)
			stack = popCompletedScopes(stack, braceDepth)
			continue
		}

		if matches := kotlinImportPattern.FindStringSubmatch(trimmed); len(matches) >= 2 {
			importedName := strings.TrimSpace(matches[1])
			if importedName == "" {
				braceDepth += braceDelta(rawLine)
				stack = popCompletedScopes(stack, braceDepth)
				continue
			}
			alias := kotlinImportAlias(importedName)
			importType := "import"
			if len(matches) == 3 && strings.TrimSpace(matches[2]) != "" {
				alias = strings.TrimSpace(matches[2])
				importType = "alias"
			}
			if alias != "" {
				knownTypeNames[alias] = struct{}{}
			}
			shared.AppendBucket(payload, "imports", map[string]any{
				"name":             importedName,
				"source":           importedName,
				"alias":            alias,
				"full_import_name": strings.TrimSpace(rawLine),
				"import_type":      importType,
				"line_number":      lineNumber,
				"lang":             "kotlin",
			})
		}

		declaredTypeNames := make(map[string]struct{})
		annotationsConsumed := false
		if matches := kotlinClassPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			annotationsConsumed = true
			name := matches[1]
			knownTypeNames[name] = struct{}{}
			declaredTypeNames[name] = struct{}{}
			if typeParameters := kotlinDeclaredTypeParameters(trimmed); len(typeParameters) > 0 {
				classTypeParameters[name] = typeParameters
			}
			item := kotlinTypeItem(name, lineNumber, annotations, "class", trimmed)
			if implementedTypes := kotlinImplementedTypes(rawLine); len(implementedTypes) > 0 {
				classInterfaces[name] = make(map[string]struct{}, len(implementedTypes))
				for _, implementedType := range implementedTypes {
					classInterfaces[name][implementedType] = struct{}{}
				}
			}
			shared.AppendBucket(payload, "classes", item)
			if properties := kotlinPrimaryConstructorPropertyTypes(rawLine); len(properties) > 0 {
				if _, ok := classPropertyTypes[name]; !ok {
					classPropertyTypes[name] = make(map[string]string, len(properties))
				}
				for propertyName, propertyType := range properties {
					classPropertyTypes[name][propertyName] = propertyType
				}
			}
			stack = append(stack, scopedContext{kind: "class", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}
		if matches := kotlinObjectPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			annotationsConsumed = true
			name := matches[1]
			knownTypeNames[name] = struct{}{}
			declaredTypeNames[name] = struct{}{}
			item := kotlinTypeItem(name, lineNumber, annotations, "class", trimmed)
			shared.AppendBucket(payload, "classes", item)
			stack = append(stack, scopedContext{kind: "class", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}
		if matches := kotlinCompanionPattern.FindStringSubmatch(trimmed); len(matches) >= 1 {
			annotationsConsumed = true
			name := "Companion"
			if len(matches) == 2 && strings.TrimSpace(matches[1]) != "" {
				name = matches[1]
			}
			knownTypeNames[name] = struct{}{}
			declaredTypeNames[name] = struct{}{}
			item := kotlinTypeItem(name, lineNumber, annotations, "class", trimmed)
			shared.AppendBucket(payload, "classes", item)
			stack = append(stack, scopedContext{kind: "companion", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}
		if matches := kotlinInterfacePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			annotationsConsumed = true
			name := matches[1]
			knownTypeNames[name] = struct{}{}
			declaredTypeNames[name] = struct{}{}
			if typeParameters := kotlinDeclaredTypeParameters(trimmed); len(typeParameters) > 0 {
				classTypeParameters[name] = typeParameters
			}
			item := kotlinTypeItem(name, lineNumber, annotations, "interface", trimmed)
			shared.AppendBucket(payload, "interfaces", item)
			stack = append(stack, scopedContext{kind: "interface", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}
		if matches := kotlinEnumPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			annotationsConsumed = true
			name := matches[1]
			knownTypeNames[name] = struct{}{}
			declaredTypeNames[name] = struct{}{}
			item := kotlinTypeItem(name, lineNumber, annotations, "class", trimmed)
			shared.AppendBucket(payload, "classes", item)
			stack = append(stack, scopedContext{kind: "class", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}

		if matches := kotlinFunctionPattern.FindStringSubmatch(trimmed); len(matches) == 3 {
			annotationsConsumed = true
			name := matches[2]
			if strings.TrimSpace(name) != "" {
				item := map[string]any{
					"name":        name,
					"line_number": lineNumber,
					"end_line":    lineNumber,
					"lang":        "kotlin",
					"decorators":  []string{},
				}
				if kotlinFunctionIsSuspend(trimmed) {
					item["suspend"] = true
				}
				if typeContext := kotlinCurrentTypeScopeName(stack); typeContext != "" {
					item["class_context"] = typeContext
				}
				if receiverType := strings.TrimSpace(matches[1]); receiverType != "" {
					item["extension_receiver"] = receiverType
					if _, ok := item["class_context"]; !ok {
						item["class_context"] = receiverType
					}
				}
				typeContext := kotlinCurrentTypeScopeName(stack)
				scopeKind := currentScopedKind(stack, "class", "interface")
				if rootKinds := kotlinFunctionDeadCodeRootKinds(
					trimmed,
					annotations,
					name,
					typeContext,
					scopeKind,
					interfaceMethods,
					classInterfaces,
				); len(rootKinds) > 0 {
					item["dead_code_root_kinds"] = rootKinds
				}
				if scopeKind == "interface" && typeContext != "" {
					if _, ok := interfaceMethods[typeContext]; !ok {
						interfaceMethods[typeContext] = make(map[string]struct{})
					}
					interfaceMethods[typeContext][name] = struct{}{}
				}
				if options.IndexSource {
					item["source"] = rawLine
				}
				shared.AppendBucket(payload, "functions", item)
				if receiverType, functionName, returnType := kotlinFunctionDeclarationReturnType(trimmed); functionName != "" && returnType != "" {
					key := functionName
					if receiverType != "" {
						key = receiverType + "." + functionName
					} else if typeContext := kotlinCurrentTypeScopeName(stack); typeContext != "" {
						key = typeContext + "." + functionName
					}
					kotlinStoreFunctionReturnType(functionReturnTypes, packageName, key, returnType)
				}
				if strings.Contains(rawLine, "{") {
					stack = append(stack, scopedContext{
						kind:       "function",
						name:       name,
						braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")),
					})
				}
			}
		}
		if kotlinConstructorPattern.MatchString(trimmed) {
			annotationsConsumed = true
			item := map[string]any{
				"name":             "constructor",
				"line_number":      lineNumber,
				"end_line":         lineNumber,
				"constructor_kind": "secondary",
				"lang":             "kotlin",
				"decorators":       []string{},
			}
			if typeContext := kotlinCurrentTypeScopeName(stack); typeContext != "" {
				item["class_context"] = typeContext
			}
			item["dead_code_root_kinds"] = kotlinConstructorDeadCodeRootKinds()
			if options.IndexSource {
				item["source"] = rawLine
			}
			shared.AppendBucket(payload, "functions", item)
		}

		if matches := kotlinVariablePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			annotationsConsumed = true
			name := matches[1]
			functionContext := currentScopedName(stack, "function")
			typeContext := kotlinCurrentTypeScopeName(stack)
			effectiveVariableTypes := localVariableTypes[functionContext]
			if functionContext != "" {
				effectiveVariableTypes = kotlinMergeVariableTypes(
					localVariableTypes[functionContext],
					kotlinActiveSmartCastTypes(smartCastScopes, functionContext),
					kotlinInlineSmartCastTypes(trimmed, functionContext, whenSubjectScopes),
				)
			}
			if typedType := kotlinTypedDeclarationType(trimmed); typedType != "" {
				switch {
				case functionContext != "":
					if _, ok := localVariableTypes[functionContext]; !ok {
						localVariableTypes[functionContext] = make(map[string]string)
					}
					localVariableTypes[functionContext][name] = typedType
				case typeContext != "":
					if _, ok := classPropertyTypes[typeContext]; !ok {
						classPropertyTypes[typeContext] = make(map[string]string)
					}
					classPropertyTypes[typeContext][name] = typedType
				}
			} else if functionContext != "" {
				if _, ok := localVariableTypes[functionContext]; !ok {
					localVariableTypes[functionContext] = make(map[string]string)
				}
				if inferredType := kotlinInferAssignedVariableType(
					trimmed,
					name,
					functionContext,
					typeContext,
					packageName,
					classTypeParameters,
					map[string]map[string]string{functionContext: effectiveVariableTypes},
					classPropertyTypes,
					functionReturnTypes,
				); inferredType != "" {
					localVariableTypes[functionContext][name] = inferredType
					if inferredKind := kotlinInferAssignedVariableCallKind(trimmed, name); inferredKind != "" {
						if _, ok := localVariableCallKinds[functionContext]; !ok {
							localVariableCallKinds[functionContext] = make(map[string]string)
						}
						localVariableCallKinds[functionContext][name] = inferredKind
					} else {
						delete(localVariableCallKinds[functionContext], name)
					}
				}
			}
			if _, ok := seenVariables[name]; !ok {
				seenVariables[name] = struct{}{}
				shared.AppendBucket(payload, "variables", map[string]any{
					"name":        name,
					"line_number": lineNumber,
					"end_line":    lineNumber,
					"lang":        "kotlin",
				})
			}
		}

		functionDeclCutoff := -1
		if kotlinFunctionPattern.MatchString(trimmed) {
			if idx := strings.Index(trimmed, "="); idx >= 0 {
				functionDeclCutoff = idx
			}
			if idx := strings.Index(trimmed, "{"); idx >= 0 && (functionDeclCutoff < 0 || idx < functionDeclCutoff) {
				functionDeclCutoff = idx
			}
		}

		functionContext := currentScopedName(stack, "function")
		currentTypeContext := kotlinCurrentTypeScopeName(stack)
		effectiveVariableTypes := localVariableTypes[functionContext]
		if functionContext != "" {
			effectiveVariableTypes = kotlinMergeVariableTypes(
				localVariableTypes[functionContext],
				kotlinActiveSmartCastTypes(smartCastScopes, functionContext),
				kotlinInlineSmartCastTypes(trimmed, functionContext, whenSubjectScopes),
			)
		}

		seenLineCalls := make(map[string]struct{})
		if matches := kotlinInfixCallPattern.FindStringSubmatch(trimmed); len(matches) == 4 {
			receiver := matches[1]
			name := matches[2]
			if kotlinCallNameAllowed(name) {
				if functionContext != "" {
					var (
						inferredType string
						classContext string
					)
					if receiver == "this" {
						if currentType := currentTypeContext; currentType != "" {
							classContext = currentType
							inferredType = currentType
						}
					} else {
						inferredType = kotlinInferReceiverType(
							receiver,
							effectiveVariableTypes,
							classPropertyTypes,
							currentTypeContext,
							packageName,
							functionReturnTypes,
							classTypeParameters,
						)
					}
					if inferredType != "" {
						fullName := strings.TrimSpace(receiver + " " + name)
						callKey := fullName + "#" + strconv.Itoa(lineNumber)
						if _, ok := seenLineCalls[callKey]; !ok {
							seenLineCalls[callKey] = struct{}{}
							item := map[string]any{
								"name":              name,
								"full_name":         fullName,
								"line_number":       lineNumber,
								"lang":              "kotlin",
								"inferred_obj_type": kotlinBaseTypeName(inferredType),
							}
							if classContext != "" {
								item["class_context"] = classContext
							}
							shared.AppendBucket(payload, "function_calls", item)
						}
					}
				}
			}
		}
		kotlinAppendThisCalls(payload, trimmed, lineNumber, seenLineCalls, currentTypeContext)

		kotlinAppendConstructorCalls(
			payload,
			trimmed,
			lineNumber,
			functionDeclCutoff,
			seenLineCalls,
			knownTypeNames,
			kotlinClassPattern.MatchString(trimmed) ||
				kotlinObjectPattern.MatchString(trimmed) ||
				kotlinCompanionPattern.MatchString(trimmed) ||
				kotlinInterfacePattern.MatchString(trimmed) ||
				kotlinEnumPattern.MatchString(trimmed),
		)

		callTrimmed := strings.ReplaceAll(trimmed, "?.", ".")
		callTrimmed = kotlinStripReceiverPreservingScopeFunctions(callTrimmed)
		kotlinAppendCastReceiverCalls(payload, callTrimmed, lineNumber, functionDeclCutoff, seenLineCalls)
		normalizedTrimmed := kotlinNormalizeParenthesizedReceivers(callTrimmed)
		for _, match := range kotlinCallPattern.FindAllStringSubmatchIndex(normalizedTrimmed, -1) {
			if len(match) != 6 {
				continue
			}
			if functionDeclCutoff >= 0 && match[0] < functionDeclCutoff {
				continue
			}
			receiver := strings.TrimSuffix(strings.TrimSpace(normalizedTrimmed[match[2]:match[3]]), ".")
			name := normalizedTrimmed[match[4]:match[5]]
			fullName := strings.TrimSpace(normalizedTrimmed[match[2]:match[3]] + "." + normalizedTrimmed[match[4]:match[5]])
			for _, chainedCall := range kotlinExpandChainedCalls(receiver, name, fullName) {
				receiver := chainedCall.Receiver
				name := chainedCall.Name
				if !kotlinCallNameAllowed(name) {
					continue
				}
				if receiver == "" {
					if _, declared := declaredTypeNames[name]; declared {
						continue
					}
				}
				callKey := chainedCall.FullName + "#" + strconv.Itoa(lineNumber)
				if _, ok := seenLineCalls[callKey]; ok {
					continue
				}
				seenLineCalls[callKey] = struct{}{}
				item := map[string]any{
					"name":        name,
					"full_name":   chainedCall.FullName,
					"line_number": lineNumber,
					"lang":        "kotlin",
				}
				if receiver == "this" {
					if typeContext := currentTypeContext; typeContext != "" {
						item["class_context"] = typeContext
					}
				} else if receiver != "" {
					if functionContext != "" {
						if inferredType := kotlinInferReceiverType(
							receiver,
							effectiveVariableTypes,
							classPropertyTypes,
							currentTypeContext,
							packageName,
							functionReturnTypes,
							classTypeParameters,
						); inferredType != "" {
							item["inferred_obj_type"] = kotlinBaseTypeName(inferredType)
						}
						if callKind := kotlinInferReceiverCallKind(receiver, localVariableCallKinds[functionContext]); callKind != "" {
							item["call_kind"] = callKind
						}
					}
				}
				shared.AppendBucket(payload, "function_calls", item)
			}
		}

		if functionContext != "" {
			if scopedTypes := kotlinScopedSmartCastTypes(trimmed); len(scopedTypes) > 0 && strings.Contains(rawLine, "{") {
				smartCastScopes = append(smartCastScopes, kotlinTypeFlowScope{
					functionName:  functionContext,
					braceDepth:    braceDepth + max(1, strings.Count(rawLine, "{")),
					variableTypes: scopedTypes,
				})
			}
			if subject := kotlinWhenSubject(trimmed); subject != "" && strings.Contains(rawLine, "{") {
				whenSubjectScopes = append(whenSubjectScopes, kotlinWhenSubjectScope{
					functionName: functionContext,
					braceDepth:   braceDepth + max(1, strings.Count(rawLine, "{")),
					subject:      subject,
				})
			}
		}

		braceDepth += braceDelta(rawLine)
		stack = popCompletedScopes(stack, braceDepth)
		if annotationsConsumed {
			pendingAnnotations = pendingAnnotations[:0]
		}
	}

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "interfaces")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")

	return payload, nil
}
