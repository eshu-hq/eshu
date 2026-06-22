package haskell

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// haskellParitySource exercises every symbol bucket the line-scan extractor
// populated: a module header with an explicit export list, qualified and named
// imports, data/newtype/type/data-family declarations, a typeclass with a
// constrained context, an instance with methods, top-level signatures and
// functions (including a no-parameter bind and a guarded multi-clause function),
// a where block with local bindings, and a top-level variable.
const haskellParitySource = `module Demo.App
  ( main
  , run
  , Worker(..)
  , Runner(..)
  ) where

import qualified Data.Text as T
import Data.List (nub, sort)

data Worker = Worker
newtype Wrapper a = Wrapper a
type Alias = Int
data family Fam a

class (Show a) => Runner a where
  runTask :: a -> IO ()

instance Runner Worker where
  runTask worker = run worker

main :: IO ()
main = run Worker

run :: Worker -> String
run worker = helper (T.pack (show worker))
  where
    helper value = T.unpack value

topVar = 42

caller value
  | value > 0 = run value
  | otherwise = run 0
`

// canonicalPayload renders the parser payload buckets as deterministic JSON so a
// byte-parity snapshot can lock the line-scan output before the AST migration
// and confirm the AST extractor reproduces identical keys, value shapes, and
// ordering afterward. HTML escaping is disabled so operators such as ">" in
// guarded function source stay readable instead of becoming ">".
func canonicalPayload(t *testing.T, payload map[string]any) string {
	t.Helper()

	var builder strings.Builder
	encoder := json.NewEncoder(&builder)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func parseParity(t *testing.T) string {
	t.Helper()

	path := writeSource(t, "Parity.hs", haskellParitySource)
	payload, err := Parse(path, false, shared.Options{IndexSource: true})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	// path is a t.TempDir absolute path; drop it so the snapshot is stable.
	delete(payload, "path")
	return canonicalPayload(t, payload)
}

// TestHaskellPayloadParitySnapshot pins the full payload byte-for-byte. The
// golden string was captured from the line-scan extractor and must stay
// identical after the tree-sitter migration.
func TestHaskellPayloadParitySnapshot(t *testing.T) {
	t.Parallel()

	got := parseParity(t)
	if got != haskellParityGolden {
		t.Fatalf("payload parity drift:\n%s", got)
	}
}
