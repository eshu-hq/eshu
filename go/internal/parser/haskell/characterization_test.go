package haskell

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// fixtureCorpus lists the Haskell sources exercised for byte-parity. The bodies
// cover the symbol kinds the migration must reproduce: module export lists,
// imports with aliases and name lists, data/newtype/type declarations, record
// syntax, typeclasses, instances, top-level binds and functions, type
// signatures, guards, where-clause locals, and qualified calls.
var fixtureCorpus = map[string]string{
	"Basic.hs": `module Basic where

import Data.List (sort, nub, intercalate)
import Data.Char (toUpper, toLower)

greet :: String -> String
greet name = "Hello, " ++ name ++ "!"

add :: Int -> Int -> Int
add a b = a + b

classify :: Int -> String
classify n
  | n < 0     = "negative"
  | n == 0    = "zero"
  | n < 10    = "small"
  | otherwise = "large"

bmi :: Double -> Double -> String
bmi weight height = category
  where
    index = weight / height ^ 2
    category
      | index < 18.5 = "underweight"
      | index < 25.0 = "normal"
      | otherwise     = "overweight"

head' :: [a] -> Maybe a
head' []    = Nothing
head' (x:_) = Just x

processNames :: [String] -> String
processNames = intercalate ", " . sort . nub . map (map toUpper)
`,
	"Exports.hs": `module Demo.App
  ( main
  , run
  , Worker(..)
  , Runner(..)
  ) where

import qualified Data.Text as T
import Data.List (nub, sort)

data Worker = Worker

class Runner a where
  runTask :: a -> IO ()

instance Runner Worker where
  runTask worker = run worker

main = run Worker
run worker = T.unpack (T.pack (show worker))
helper worker = sort (nub [worker])
`,
	"Types.hs": `module Types where

data Shape
  = Circle Double
  | Rectangle Double Double
  deriving (Show, Eq)

data Person = Person
  { personName :: String
  , personAge  :: Int
  } deriving (Show, Eq)

newtype Name = Name String deriving (Show, Eq)

type Point = (Double, Double)

class Describable a where
  describe :: a -> String

instance Describable Shape where
  describe (Circle r) = "Circle"

area :: Shape -> Double
area (Circle r) = pi * r * r
`,
	"Where.hs": `module Where where

run task = result
  where
    result = task

withWhere item = local item
  where
    local value = helper value

helper value = value
`,
}

// TestHaskellPayloadCharacterization records the full payload for every fixture
// in the corpus and compares it against a golden snapshot. It is the byte-parity
// gate for migrating symbol extraction from line-scan to tree-sitter AST: the
// snapshot is generated from the pre-migration extractor, so the AST extractor
// must reproduce the same keys and value shapes. Regenerate with
// EShU_UPDATE_GOLDEN=1.
func TestHaskellPayloadCharacterization(t *testing.T) {
	for name, body := range fixtureCorpus {
		name, body := name, body
		t.Run(name, func(t *testing.T) {
			path := writeSource(t, name, body)
			payload, err := Parse(path, false, shared.Options{IndexSource: true})
			if err != nil {
				t.Fatalf("Parse(%s) error = %v, want nil", name, err)
			}
			got := normalizeHaskellPayload(payload)

			goldenPath := filepath.Join("testdata", "characterization", name+".json")
			if os.Getenv("ESHU_UPDATE_GOLDEN") == "1" {
				writeGolden(t, goldenPath, got)
				return
			}
			wantBytes, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with ESHU_UPDATE_GOLDEN=1)", goldenPath, err)
			}
			if string(got) != string(wantBytes) {
				t.Fatalf("payload mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, wantBytes)
			}
		})
	}
}

// normalizeHaskellPayload renders the buckets that carry symbol extraction into
// stable indented JSON. The file path key varies per temp dir, so it is dropped.
func normalizeHaskellPayload(payload map[string]any) []byte {
	view := map[string]any{}
	for _, key := range []string{"modules", "imports", "classes", "functions", "variables", "function_calls"} {
		if bucket, ok := payload[key]; ok {
			view[key] = bucket
		}
	}
	out, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(out, '\n')
}

func writeGolden(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir golden dir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write golden %s: %v", path, err)
	}
}
