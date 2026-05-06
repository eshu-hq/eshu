package parser

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	scippb "github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

func TestSCIPIndexParserParsesDefinitionsAndCallEdges(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "caller.py")
	calleePath := filepath.Join(repoRoot, "callee.py")
	writeSCIPTestFile(t, callerPath, "def handle():\n    return callee(1)\n")
	writeSCIPTestFile(t, calleePath, "def callee(x):\n    return x\n")

	callerSymbol := "pkg/caller#handle()."
	calleeSymbol := "pkg/callee#callee()."
	indexPath := filepath.Join(repoRoot, "index.scip")
	writeSCIPIndexFixture(
		t,
		indexPath,
		&scippb.Index{
			Documents: []*scippb.Document{
				{
					RelativePath: "callee.py",
					Language:     "python",
					Occurrences: []*scippb.Occurrence{
						{
							Range:       []int32{0, 0, 0, 6},
							Symbol:      calleeSymbol,
							SymbolRoles: int32(scippb.SymbolRole_Definition),
						},
					},
					Symbols: []*scippb.SymbolInformation{
						{
							Symbol:        calleeSymbol,
							DisplayName:   "callee(x: int) -> int",
							Documentation: []string{"callee docs"},
							Kind:          scippb.SymbolInformation_Function,
						},
					},
				},
				{
					RelativePath: "caller.py",
					Language:     "python",
					Occurrences: []*scippb.Occurrence{
						{
							Range:       []int32{0, 0, 0, 6},
							Symbol:      callerSymbol,
							SymbolRoles: int32(scippb.SymbolRole_Definition),
						},
						{
							Range:  []int32{1, 11, 1, 17},
							Symbol: calleeSymbol,
						},
					},
					Symbols: []*scippb.SymbolInformation{
						{
							Symbol:      callerSymbol,
							DisplayName: "handle() -> int",
							Kind:        scippb.SymbolInformation_Function,
						},
					},
				},
			},
		},
	)

	got, err := (SCIPIndexParser{}).Parse(indexPath, repoRoot)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	if len(got.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(got.Files))
	}

	callerFile := got.Files[callerPath]
	functions, _ := callerFile["functions"].([]map[string]any)
	if len(functions) != 1 {
		t.Fatalf("len(caller functions) = %d, want 1", len(functions))
	}
	if got, want := functions[0]["name"], "handle"; got != want {
		t.Fatalf("caller function name = %#v, want %#v", got, want)
	}
	if got, want := functions[0]["return_type"], "int"; got != want {
		t.Fatalf("caller return_type = %#v, want %#v", got, want)
	}
	if got, want := functions[0]["args"].([]string), []string{}; !reflect.DeepEqual(got, want) {
		t.Fatalf("caller args = %#v, want %#v", got, want)
	}

	callEdges, _ := callerFile["function_calls_scip"].([]map[string]any)
	if len(callEdges) != 1 {
		t.Fatalf("len(function_calls_scip) = %d, want 1", len(callEdges))
	}
	if got, want := callEdges[0]["callee_name"], "callee"; got != want {
		t.Fatalf("callee_name = %#v, want %#v", got, want)
	}
	if got, want := callEdges[0]["callee_file"], calleePath; got != want {
		t.Fatalf("callee_file = %#v, want %#v", got, want)
	}

	symbol := got.SymbolTable[calleeSymbol]
	if got, want := symbol["display_name"], "callee(x: int) -> int"; got != want {
		t.Fatalf("symbol display_name = %#v, want %#v", got, want)
	}
	if got, want := symbol["file"], "callee.py"; got != want {
		t.Fatalf("symbol file = %#v, want %#v", got, want)
	}
}

func writeSCIPIndexFixture(t *testing.T, path string, index *scippb.Index) {
	t.Helper()

	payload, err := proto.Marshal(index)
	if err != nil {
		t.Fatalf("proto.Marshal() error = %v, want nil", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v, want nil", path, err)
	}
}

func writeSCIPTestFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := ensureParentDirectory(path); err != nil {
		t.Fatalf("ensureParentDirectory(%q) error = %v, want nil", path, err)
	}
	if err := osWriteFile(path, []byte(body)); err != nil {
		t.Fatalf("osWriteFile(%q) error = %v, want nil", path, err)
	}
}
