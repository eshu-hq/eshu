package golang

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goAnnotateCallChainMetadata(
	item map[string]any,
	callNode *tree_sitter.Node,
	functionNode *tree_sitter.Node,
	source []byte,
	localReceiverBindings []goLocalReceiverBinding,
) {
	receiverIdentifier, receiverMethod := goMethodReturnChainReceiver(functionNode, source)
	if receiverIdentifier == "" || receiverMethod == "" {
		return
	}
	receiverType := goInferredReceiverType(receiverIdentifier, nodeLine(callNode), localReceiverBindings)
	if receiverType == "" {
		return
	}
	item["chain_receiver_identifier"] = receiverIdentifier
	item["chain_receiver_method"] = receiverMethod
	item["chain_receiver_obj_type"] = receiverType
}

func goMethodReturnChainReceiver(functionNode *tree_sitter.Node, source []byte) (string, string) {
	if functionNode == nil || functionNode.Kind() != "selector_expression" {
		return "", ""
	}
	receiverCall := goUnwrapSingleExpression(functionNode.ChildByFieldName("operand"))
	if receiverCall == nil || receiverCall.Kind() != "call_expression" {
		return "", ""
	}
	receiverFunction := receiverCall.ChildByFieldName("function")
	if receiverFunction == nil || receiverFunction.Kind() != "selector_expression" {
		return "", ""
	}
	baseNode := receiverFunction.ChildByFieldName("operand")
	methodNode := receiverFunction.ChildByFieldName("field")
	if baseNode == nil || methodNode == nil || baseNode.Kind() != "identifier" {
		return "", ""
	}
	return strings.TrimSpace(nodeText(baseNode, source)), strings.TrimSpace(nodeText(methodNode, source))
}
