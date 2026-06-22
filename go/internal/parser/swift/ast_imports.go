package swift

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// emitImport appends one import row for an import_declaration node. The dotted
// module name is the text of the `identifier` child (e.g. "Foundation" or
// "os.log"). full_import_name mirrors name; alias and context stay nil to match
// the established Swift import payload shape.
func (b *swiftPayloadBuilder) emitImport(node *tree_sitter.Node, source []byte) {
	identifier := swiftChildByKind(node, "identifier")
	if identifier == nil {
		return
	}
	name := swiftTrimText(identifier, source)
	if name == "" {
		return
	}
	shared.AppendBucket(b.payload, "imports", map[string]any{
		"name":             name,
		"full_import_name": name,
		"alias":            nil,
		"context":          nil,
		"is_dependency":    b.isDependency,
		"line_number":      shared.NodeLine(node),
		"lang":             "swift",
	})
}
