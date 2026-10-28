// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package symbols

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestInsideFunctionDistinguishesNestedFromPackageScope(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nvar top = 1\n\nfunc run() {\n\tx := 1\n\t_ = x\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	lookup := BuildParentLookup(root)

	var inside, outside *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "identifier" {
			return
		}
		text := string(source[node.StartByte():node.EndByte()])
		switch text {
		case "x":
			if inside == nil {
				inside = node
			}
		case "top":
			if outside == nil {
				outside = node
			}
		}
	})
	if inside == nil || outside == nil {
		t.Fatal("fixture did not yield both identifiers")
	}
	if !InsideFunction(inside, lookup) {
		t.Error("identifier inside run is not reported inside a function")
	}
	if InsideFunction(outside, lookup) {
		t.Error("package-level identifier is reported inside a function")
	}
}

func TestVariableNamesPayloadShape(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nvar client = 1\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	var spec *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if spec == nil && node.Kind() == "var_spec" {
			spec = node
		}
	})
	if spec == nil {
		t.Fatal("no var_spec in fixture")
	}

	got := VariableNames(spec, source)
	if len(got) != 1 {
		t.Fatalf("VariableNames() returned %d payloads, want 1", len(got))
	}
	if got[0]["name"] != "client" || got[0]["lang"] != "go" {
		t.Errorf("payload = %#v, want name=client lang=go", got[0])
	}
	if _, ok := got[0]["line_number"]; !ok {
		t.Errorf("payload = %#v, want line_number key", got[0])
	}

	var ident *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if ident == nil && node.Kind() == "identifier" {
			ident = node
		}
	})
	if got := VariableNames(ident, source); len(got) != 0 {
		t.Errorf("VariableNames(identifier) = %#v, want empty (no name field)", got)
	}
}

func TestShortVariableNamesEmitsPayloadPerLeftIdentifier(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nfunc run() {\n\tx, y := 1, 2\n\t_, z := f()\n\t_ = x\n\t_ = y\n\t_ = z\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	names := []string{}
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if node.Kind() != "short_var_declaration" {
			return
		}
		for _, item := range ShortVariableNames(node, source) {
			name, _ := item["name"].(string)
			names = append(names, name)
		}
	})
	// Every identifier on the left gets a payload, including the blank;
	// only non-identifier targets are skipped.
	if !reflect.DeepEqual(names, []string{"x", "y", "_", "z"}) {
		t.Errorf("short variable names = %#v, want [x y _ z]", names)
	}

	var ident *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if ident == nil && node.Kind() == "identifier" {
			ident = node
		}
	})
	if got := ShortVariableNames(ident, source); len(got) != 0 {
		t.Errorf("ShortVariableNames(identifier) = %#v, want empty (no left field)", got)
	}
}

func TestDocstringCollectsPrecedingComments(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\n// First line.\n// Second line.\nfunc documented() {}\n\nfunc bare() {}\n\n// Detached.\n\nfunc gapped() {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()

	texts := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "function_declaration" {
			return
		}
		name := node.ChildByFieldName("name")
		texts[string(source[name.StartByte():name.EndByte()])] = Docstring(node, source)
	})

	if got := texts["documented"]; got != "First line.\nSecond line." {
		t.Errorf("documented docstring = %q, want both lines joined", got)
	}
	if got := texts["bare"]; got != "" {
		t.Errorf("bare docstring = %q, want empty", got)
	}
	if got := texts["gapped"]; got != "" {
		t.Errorf("gapped docstring = %q, want empty (blank line breaks contiguity)", got)
	}
	if got := Docstring(nil, source); got != "" {
		t.Errorf("Docstring(nil) = %q, want empty", got)
	}
}

func TestDocstringReadsBlockComment(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\n/* Block note. */\nfunc blocked() {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	var funcNode *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if funcNode == nil && node.Kind() == "function_declaration" {
			funcNode = node
		}
	})
	if got := Docstring(funcNode, source); got != "Block note." {
		t.Errorf("block docstring = %q, want %q", got, "Block note.")
	}
}

func TestReceiverContextReadsMethodReceivers(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype S struct{}\n\nfunc (s *S) Ptr() {}\n\nfunc (s S) Val() {}\n\nfunc plain() {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()

	contexts := map[string]string{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "method_declaration", "function_declaration":
			name := node.ChildByFieldName("name")
			contexts[string(source[name.StartByte():name.EndByte()])] = ReceiverContext(node, source)
		}
	})

	if contexts["Ptr"] != "S" || contexts["Val"] != "S" {
		t.Errorf("receiver contexts = %v, want S for both methods", contexts)
	}
	if contexts["plain"] != "" {
		t.Errorf("plain function context = %q, want empty", contexts["plain"])
	}
	if got := ReceiverContext(nil, source); got != "" {
		t.Errorf("ReceiverContext(nil) = %q, want empty", got)
	}
}

func TestIdentifierIsExportedTable(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"Foo": true, "FOO": true, "FooBar": true,
		"foo": false, "_Foo": false, "": false, "verständlich": false,
	}
	for input, want := range cases {
		if got := IdentifierIsExported(input); got != want {
			t.Errorf("IdentifierIsExported(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestIdentifierNamesLowercasesEveryIdentifierKind(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\ntype Store struct {\n\tClient Client\n}\n\nfunc use(s Store) {}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	var structNode *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if structNode == nil && node.Kind() == "struct_type" {
			structNode = node
		}
	})
	if structNode == nil {
		t.Fatal("no struct_type in fixture")
	}
	got := IdentifierNames(structNode, source)
	found := map[string]bool{}
	for _, name := range got {
		found[name] = true
	}
	for _, want := range []string{"client"} {
		if !found[want] {
			t.Errorf("IdentifierNames(struct) = %#v, want %q included", got, want)
		}
	}
	if got := IdentifierNames(nil, source); len(got) != 0 {
		t.Errorf("IdentifierNames(nil) = %#v, want empty", got)
	}
}

func TestSelectorBaseAndFieldSplitsSelector(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nfunc run() {\n\tfmt.Println(\"x\")\n\tplain()\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	var selector, call *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "selector_expression":
			if selector == nil {
				selector = node
			}
		case "call_expression":
			if call == nil {
				call = node
			}
		}
	})
	if selector == nil || call == nil {
		t.Fatal("fixture did not yield selector and call")
	}

	base, field, ok := SelectorBaseAndField(selector, source)
	if !ok || base != "fmt" || field != "Println" {
		t.Errorf("SelectorBaseAndField = (%q, %q, %v), want (fmt, Println, true)", base, field, ok)
	}
	if _, _, ok := SelectorBaseAndField(call, source); ok {
		t.Error("call_expression classified as selector, want ok=false")
	}
	if _, _, ok := SelectorBaseAndField(nil, source); ok {
		t.Error("nil node classified as selector, want ok=false")
	}
}

func TestUnwrapSingleExpression(t *testing.T) {
	t.Parallel()

	// Only an expression_list with exactly one named child unwraps; a
	// parenthesized expression is returned as-is. The receive in
	// `v := <-ch` parses as a single-child expression_list.
	source := []byte("package p\n\nfunc run(ch chan int) {\n\tx, y := 1, 2\n\tv := <-ch\n\t_, _, _ = x, y, v\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()

	single, multi := 0, 0
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if node.Kind() != "expression_list" {
			return
		}
		cursor := node.Walk()
		defer cursor.Close()
		children := node.NamedChildren(cursor)
		got := UnwrapSingleExpression(node)
		switch len(children) {
		case 1:
			single++
			// The sole named child must come back, whatever its kind.
			if got == node || got.StartByte() != children[0].StartByte() || got.EndByte() != children[0].EndByte() {
				t.Errorf("single-child list did not unwrap to its child: kind=%s", got.Kind())
			}
		default:
			multi++
			if got != node {
				t.Errorf("multi-child list was unwrapped, want it returned as-is")
			}
		}
	})
	if single == 0 || multi == 0 {
		t.Errorf("fixture yielded single=%d multi=%d expression lists, want at least 1 of each", single, multi)
	}
	var ident *tree_sitter.Node
	shared.WalkNamed(tree.RootNode(), func(node *tree_sitter.Node) {
		if ident == nil && node.Kind() == "identifier" {
			ident = node
		}
	})
	if got := UnwrapSingleExpression(ident); got != ident {
		t.Error("non-list node was not returned as-is")
	}
	if got := UnwrapSingleExpression(nil); got != nil {
		t.Errorf("nil unwrap = %#v, want nil", got)
	}
}

func TestNameIsLocallyBoundRespectsScope(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nfunc run(a int, b int) {\n\tuse(a, b)\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()

	var funcNode, callNode *tree_sitter.Node
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration":
			funcNode = node
		case "call_expression":
			callNode = node
		}
	})
	bindings := LocalNameBindingsFromParameters(funcNode, source)
	callLine := shared.NodeLine(callNode)

	if !NameIsLocallyBound("a", callLine, bindings) {
		t.Error("parameter a not reported bound at the call line")
	}
	if NameIsLocallyBound("missing", callLine, bindings) {
		t.Error("unknown name reported bound")
	}
	if NameIsLocallyBound("a", 1, bindings) {
		t.Error("parameter reported bound above its declaration line")
	}
	if NameIsLocallyBound("", callLine, bindings) || NameIsLocallyBound("a", 0, bindings) {
		t.Error("blank name or zero line reported bound")
	}
}

func TestFunctionLiteralIsCompositeElement(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nvar registry = []func(){\n\tfunc() {},\n}\n\nfunc run() {\n\tlocal := func() {}\n\t_ = local\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	lookup := BuildParentLookup(root)

	literals := []*tree_sitter.Node{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "func_literal" {
			literals = append(literals, node)
		}
	})
	if len(literals) != 2 {
		t.Fatalf("found %d func literals, want 2", len(literals))
	}
	if !FunctionLiteralIsCompositeElement(literals[0], lookup) {
		t.Error("registry func literal not recognized as composite element")
	}
	if FunctionLiteralIsCompositeElement(literals[1], lookup) {
		t.Error("assigned local closure recognized as composite element")
	}
}

func TestCallArgumentNodesListsDirectArguments(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nfunc run() {\n\tf(a, b)\n\tg()\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()

	counts := []int{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() == "call_expression" {
			counts = append(counts, len(CallArgumentNodes(node)))
		}
	})
	if !reflect.DeepEqual(counts, []int{2, 0}) {
		t.Errorf("argument counts = %v, want [2 0]", counts)
	}
	if got := CallArgumentNodes(root); len(got) != 0 {
		t.Errorf("CallArgumentNodes(root) = %d nodes, want empty", len(got))
	}
}

func TestCallIsFmtFormattingResolvesThroughAliasIndex(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nimport \"fmt\"\n\nfunc run() {\n\tfmt.Sprintf(\"%d\", 1)\n\tfmt.Sprintln(\"x\")\n\tfmt.Println(\"y\")\n\tplain(1)\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	index := ImportAliasIndex(root, source)

	formatting := map[string]bool{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		formatting[string(source[fn.StartByte():fn.EndByte()])] = CallIsFmtFormatting(node, source, index)
	})

	// Only the fmt string-formatting verbs count; Println is not one.
	if !formatting["fmt.Sprintf"] {
		t.Error("fmt.Sprintf not recognized as formatting")
	}
	if !formatting["fmt.Sprintln"] {
		t.Error("fmt.Sprintln not recognized as formatting")
	}
	if formatting["fmt.Println"] {
		t.Error("fmt.Println recognized as formatting, want false")
	}
	if formatting["plain"] {
		t.Error("plain call recognized as fmt formatting")
	}
}

func TestFmtStringerFirstValueArgIndexTable(t *testing.T) {
	t.Parallel()

	source := []byte("package p\n\nimport \"fmt\"\n\nfunc run(w Writer) {\n\tfmt.Sprint(\"a\")\n\tfmt.Sprintf(\"%d\", 1)\n\tfmt.Fprintf(w, \"%d\", 1)\n\tother(1)\n}\n")
	tree := parseGoLocalVariableTypesTestSource(t, source)
	defer tree.Close()
	root := tree.RootNode()
	index := ImportAliasIndex(root, source)

	indexes := map[string]int{}
	shared.WalkNamed(root, func(node *tree_sitter.Node) {
		if node.Kind() != "call_expression" {
			return
		}
		fn := node.ChildByFieldName("function")
		indexes[string(source[fn.StartByte():fn.EndByte()])] = FmtStringerFirstValueArgIndex(node, source, index)
	})

	for call, want := range map[string]int{"fmt.Sprint": 0, "fmt.Sprintf": 1, "fmt.Fprintf": 2, "other": 0} {
		if got := indexes[call]; got != want {
			t.Errorf("value arg index for %s = %d, want %d", call, got, want)
		}
	}
}
