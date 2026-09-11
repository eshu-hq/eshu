// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

var cypherScanBuildTags = []string{
	"",
	"ifafaultinjection",
	"ifadeterminismteeth",
	"ifafaultinjection,ifadeterminismteeth",
}

var cypherCallBoundaries = map[string]struct{}{
	"CypherReader.QueryCypherExists":                               {},
	"Executor.Execute":                                             {},
	"GroupExecutor.ExecuteGroup":                                   {},
	"KustomizeOverlayResolver.ListKustomizeOverlays":               {},
	"OrphanSweepReader.Run":                                        {},
	"PhaseGroupExecutor.ExecutePhaseGroup":                         {},
	"PostureExistenceReader.Run":                                   {},
	"ProbeExecutor.ExecuteProbe":                                   {},
	"ProvenanceCountReader.Run":                                    {},
	"TerraformStateConfigMatchResolver.CountConfigMatchCandidates": {},
	"TerraformStateOwnershipResolver.ResolveOwningRepoID":          {},
}

func parseCypherPackage(t *testing.T, cypherDir string) *cypherPackageSource {
	t.Helper()

	root := newCypherPackageSource()
	for _, tags := range cypherScanBuildTags {
		loaded := loadTypedPackages(t, cypherDir, tags, ".")
		configuration := buildCypherSource(t, loaded[0], nil)
		root.configurations = append(root.configurations, configuration)
	}
	return root
}

func parseCypherPackageWithReducerPorts(
	t *testing.T,
	goRoot string,
) (*cypherPackageSource, map[string]struct{}) {
	t.Helper()

	root := newCypherPackageSource()
	ports := map[string]struct{}{}
	for _, tags := range cypherScanBuildTags {
		loaded := loadTypedPackages(
			t,
			goRoot,
			tags,
			"./internal/storage/cypher",
			"./internal/reducer/...",
		)
		var cypherPackage *packages.Package
		var reducerPackages []*packages.Package
		for _, pkg := range loaded {
			switch {
			case strings.HasSuffix(pkg.PkgPath, "/internal/storage/cypher"):
				cypherPackage = pkg
			case strings.Contains(pkg.PkgPath, "/internal/reducer"):
				reducerPackages = append(reducerPackages, pkg)
			}
		}
		if cypherPackage == nil || len(reducerPackages) == 0 {
			t.Fatalf("typed port scan loaded cypher=%t reducer_packages=%d", cypherPackage != nil, len(reducerPackages))
		}
		reducerPorts := collectReducerPortSignatures(reducerPackages)
		for name := range reducerPorts {
			ports[name] = struct{}{}
		}
		configuration := buildCypherSource(t, cypherPackage, reducerPorts)
		root.configurations = append(root.configurations, configuration)
	}
	return root, ports
}

func loadTypedPackages(
	t *testing.T,
	dir string,
	tags string,
	patterns ...string,
) []*packages.Package {
	t.Helper()

	config := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo,
		Dir:   dir,
		Env:   append(os.Environ(), "GOWORK=off"),
		Tests: false,
	}
	if tags != "" {
		config.BuildFlags = []string{"-tags=" + tags}
	}
	loaded, err := packages.Load(config, patterns...)
	if err != nil {
		t.Fatalf("load %s with tags %q: %v", strings.Join(patterns, ", "), tags, err)
	}
	if len(loaded) == 0 {
		t.Fatalf("load %s with tags %q returned no packages", strings.Join(patterns, ", "), tags)
	}
	for _, pkg := range loaded {
		if len(pkg.Errors) > 0 {
			var messages []string
			for _, loadErr := range pkg.Errors {
				messages = append(messages, loadErr.Error())
			}
			t.Fatalf("load %s with tags %q:\n%s", pkg.PkgPath, tags, strings.Join(messages, "\n"))
		}
	}
	return loaded
}

func newCypherPackageSource() *cypherPackageSource {
	return &cypherPackageSource{
		bodyCalls:       map[string][]string{},
		bodyUnknownRefs: map[string][]string{},
		bodyLiterals:    map[string][]string{},
		keysByName:      map[string][]string{},
		fileByKey:       map[string]string{},
	}
}

func collectReducerPortSignatures(
	packages []*packages.Package,
) map[string][]*types.Signature {
	ports := map[string][]*types.Signature{}
	for _, pkg := range packages {
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			typeName, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			iface, ok := typeName.Type().Underlying().(*types.Interface)
			if !ok {
				continue
			}
			iface.Complete()
			for index := 0; index < iface.NumMethods(); index++ {
				method := iface.Method(index)
				if !method.Exported() || isReducerNonPortTaxonomyMethod(method) {
					continue
				}
				signature := receiverlessSignature(method)
				ports[method.Name()] = appendDistinctSignature(ports[method.Name()], signature)
			}
		}
	}
	return ports
}

func isReducerNonPortTaxonomyMethod(method *types.Func) bool {
	signature := method.Type().(*types.Signature)
	if signature.Params().Len() != 0 || signature.Results().Len() != 1 {
		return false
	}
	result, ok := types.Unalias(signature.Results().At(0).Type()).(*types.Basic)
	if !ok {
		return false
	}
	switch method.Name() {
	case "Error", "FailureClass":
		return result.Kind() == types.String
	case "Retryable":
		return result.Kind() == types.Bool
	default:
		return false
	}
}

func appendDistinctSignature(
	signatures []*types.Signature,
	candidate *types.Signature,
) []*types.Signature {
	for _, existing := range signatures {
		if types.IdenticalIgnoreTags(existing, candidate) {
			return signatures
		}
	}
	return append(signatures, candidate)
}

func receiverlessSignature(fn *types.Func) *types.Signature {
	signature := fn.Type().(*types.Signature)
	return types.NewSignatureType(
		nil,
		nil,
		nil,
		signature.Params(),
		signature.Results(),
		signature.Variadic(),
	)
}

func buildCypherSource(
	t *testing.T,
	pkg *packages.Package,
	reducerPorts map[string][]*types.Signature,
) *cypherPackageSource {
	t.Helper()

	source := newCypherPackageSource()
	if reducerPorts != nil {
		source.rootKeysByPort = map[string][]string{}
	}
	keysByObject := map[*types.Func]string{}
	stringValuesByObject := map[types.Object]string{}
	unknownStringObjects := map[types.Object]string{}
	matchedSignatures := map[string][]*types.Signature{}
	for _, file := range pkg.Syntax {
		for _, declaration := range file.Decls {
			switch typedDeclaration := declaration.(type) {
			case *ast.GenDecl:
				collectTypedPackageStrings(
					pkg,
					typedDeclaration,
					stringValuesByObject,
					unknownStringObjects,
				)
			case *ast.FuncDecl:
				if typedDeclaration.Body == nil {
					continue
				}
				fn, ok := pkg.TypesInfo.Defs[typedDeclaration.Name].(*types.Func)
				if !ok {
					t.Fatalf("function %s has no type object", typedDeclaration.Name.Name)
				}
				key, err := typedFunctionKey(fn)
				if err != nil {
					t.Fatalf("key %s: %v", typedDeclaration.Name.Name, err)
				}
				keysByObject[fn] = key
				source.fileByKey[key] = filepath.Base(pkg.Fset.Position(typedDeclaration.Pos()).Filename)
				source.keysByName[fn.Name()] = appendUnique(source.keysByName[fn.Name()], key)
				if fn.Type().(*types.Signature).Recv() != nil {
					matchReducerPort(t, source, matchedSignatures, fn, key, reducerPorts[fn.Name()])
				}
			}
		}
	}

	for _, file := range pkg.Syntax {
		collectTypedFunctionBodies(
			t,
			pkg,
			file,
			source,
			keysByObject,
			stringValuesByObject,
			unknownStringObjects,
		)
	}
	if len(stringValuesByObject) == 0 || len(source.bodyCalls) == 0 {
		t.Fatalf("scanned %s and found no string values or function bodies", pkg.PkgPath)
	}
	return source
}

func matchReducerPort(
	t *testing.T,
	source *cypherPackageSource,
	matched map[string][]*types.Signature,
	fn *types.Func,
	key string,
	candidates []*types.Signature,
) {
	t.Helper()
	methodSignature := receiverlessSignature(fn)
	for _, candidate := range candidates {
		if !types.IdenticalIgnoreTags(methodSignature, candidate) {
			continue
		}
		matched[fn.Name()] = appendDistinctSignature(matched[fn.Name()], candidate)
		if len(matched[fn.Name()]) > 1 {
			t.Fatalf("reducer port %s matches incompatible cypher method signatures", fn.Name())
		}
		source.rootKeysByPort[fn.Name()] = appendUnique(source.rootKeysByPort[fn.Name()], key)
	}
}

func collectTypedFunctionBodies(
	t *testing.T,
	pkg *packages.Package,
	file *ast.File,
	source *cypherPackageSource,
	keysByObject map[*types.Func]string,
	stringValuesByObject map[types.Object]string,
	unknownStringObjects map[types.Object]string,
) {
	t.Helper()
	for _, declaration := range file.Decls {
		fnDeclaration, ok := declaration.(*ast.FuncDecl)
		if !ok || fnDeclaration.Body == nil {
			continue
		}
		fn := pkg.TypesInfo.Defs[fnDeclaration.Name].(*types.Func)
		key := keysByObject[fn]
		calls, literals, unknown, err := collectTypedBodyRefs(
			pkg,
			keysByObject,
			stringValuesByObject,
			unknownStringObjects,
			fnDeclaration.Body,
		)
		if err != nil {
			t.Fatalf("scan %s: %v", key, err)
		}
		source.bodyCalls[key] = calls
		source.bodyLiterals[key] = literals
		source.bodyUnknownRefs[key] = unknown
	}
}

func collectTypedBodyRefs(
	pkg *packages.Package,
	keysByObject map[*types.Func]string,
	stringValuesByObject map[types.Object]string,
	unknownStringObjects map[types.Object]string,
	body *ast.BlockStmt,
) ([]string, []string, []string, error) {
	var calls []string
	var literals []string
	var unknown []string
	var scanErr error
	ast.Inspect(body, func(node ast.Node) bool {
		if scanErr != nil {
			return false
		}
		switch typedNode := node.(type) {
		case *ast.CallExpr:
			fn, resolved, err := calledFunction(pkg.TypesInfo, typedNode.Fun)
			if err != nil {
				scanErr = err
				return false
			}
			if !resolved {
				return true
			}
			if fn == nil {
				object := callTargetObject(pkg.TypesInfo, typedNode.Fun)
				if isPackageCallable(pkg, object) {
					unknown = appendUnique(unknown, dynamicCallEvidence(pkg, typedNode, object))
				}
				return true
			}
			if fn.Pkg() == nil || fn.Pkg().Path() != pkg.PkgPath {
				return true
			}
			if key, ok := keysByObject[fn]; ok {
				calls = appendUnique(calls, key)
				return true
			}
			if isInterfaceMethod(fn) {
				key, keyErr := typedFunctionKey(fn)
				if keyErr != nil {
					scanErr = keyErr
					return false
				}
				if _, boundary := cypherCallBoundaries[key]; boundary {
					return true
				}
				implementations := interfaceMethodImplementations(fn, keysByObject)
				if len(implementations) == 0 {
					unknown = appendUnique(unknown, dynamicCallEvidence(pkg, typedNode, fn))
					return true
				}
				calls = mergeUnique(calls, implementations)
				return true
			}
			scanErr = fmt.Errorf("package-local call %s has no body", fn.FullName())
			return false
		case *ast.Ident:
			object := pkg.TypesInfo.ObjectOf(typedNode)
			if value, ok := stringValuesByObject[object]; ok {
				literals = append(literals, value)
			}
			if evidence, ok := unknownStringObjects[object]; ok {
				unknown = appendUnique(unknown, evidence)
			}
		case *ast.BasicLit:
			if typedNode.Kind == token.STRING {
				if value, err := strconv.Unquote(typedNode.Value); err == nil {
					literals = append(literals, value)
				}
			}
		}
		return true
	})
	return calls, literals, unknown, scanErr
}
