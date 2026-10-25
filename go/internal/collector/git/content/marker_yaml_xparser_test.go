// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// jsYAMLScript prints, for each JSON line {"body": ...} of the file in argv[1],
// the tokens js-yaml 4.1.1 (Yarn's parser) decodes under a listed key, or null
// when it rejects the body.
const jsYAMLScript = `
const fs = require('fs');
let yaml;
for (const p of ['js-yaml', '/usr/share/nodejs/js-yaml']) { try { yaml = require(p); break; } catch (e) {} }
if (!yaml) { console.log('NOPARSER'); process.exit(0); }
const listed = ['npmauthtoken', 'npmauthident', 'npmregistryserver', 'httpsproxy'];
const re = /Zq19FlowSeedAAAAAAAA\d*/g;
function collect(o, inside, set) {
  if (o === null || typeof o !== 'object') { if (inside) (String(o).match(re) || []).forEach(x => set.add(x)); return; }
  for (const [k, v] of Object.entries(o)) {
    const l = inside || listed.includes(String(k).toLowerCase());
    if (inside) (String(k).match(re) || []).forEach(x => set.add(x));
    collect(v, l, set);
  }
}
for (const line of fs.readFileSync(process.argv[1], 'utf8').split('\n').filter(Boolean)) {
  const set = new Set();
  try { yaml.loadAll(JSON.parse(line).body).forEach(d => collect(d, false, set)); console.log(JSON.stringify([...set])); }
  catch (e) { console.log('null'); }
}
`

// pyYAMLScript is jsYAMLScript for PyYAML.
const pyYAMLScript = `
import json, re, sys
try:
    import yaml
except ImportError:
    print('NOPARSER'); sys.exit(0)
listed = ['npmauthtoken', 'npmauthident', 'npmregistryserver', 'httpsproxy']
pattern = re.compile(r'Zq19FlowSeedAAAAAAAA\d*')
def collect(o, inside, out):
    if isinstance(o, dict):
        for k, v in o.items():
            if inside: out.update(pattern.findall(str(k)))
            collect(v, inside or str(k).lower() in listed, out)
    elif isinstance(o, list):
        for x in o: collect(x, inside, out)
    elif inside:
        out.update(pattern.findall(str(o)))
for line in open(sys.argv[1]):
    line = line.strip()
    if not line: continue
    out = set()
    try:
        for d in yaml.load_all(json.loads(line)['body'], Loader=yaml.SafeLoader): collect(d, False, out)
        print(json.dumps(sorted(out)))
    except Exception:
        print('null')
`

// externalTokens runs an external parser script over the bodies and returns the
// tokens it decodes under a listed key per body (nil when it rejects the body),
// or ok=false when the parser is not installed.
func externalTokens(t *testing.T, name string, args []string, file string, count int) (tokens []map[string]bool, ok bool) {
	t.Helper()
	if _, err := exec.LookPath(args[0]); err != nil {
		return nil, false
	}
	var out bytes.Buffer
	cmd := exec.Command(args[0], append(args[1:], file)...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Logf("%s failed to run: %v", name, err)
		return nil, false
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != count {
		return nil, false
	}
	tokens = make([]map[string]bool, count)
	for i, line := range lines {
		if line == "NOPARSER" {
			return nil, false
		}
		var list []string
		if line == "null" || json.Unmarshal([]byte(line), &list) != nil {
			continue
		}
		tokens[i] = map[string]bool{}
		for _, token := range list {
			tokens[i][token] = true
		}
	}
	return tokens, true
}

// TestRedactMarkerNoParserDecodedCredentialSurvives is the cross-parser form of
// the oracle. Every token that yaml.v3, js-yaml 4.1.1 (the parser Yarn reads
// .yarnrc.yml with), or PyYAML decodes under a listed key, in a body the
// redactor accepts, must be gone from the stored body. It skips a parser that
// is not installed, and every body that the three agree on is covered by each
// run of the in-process oracle.
func TestRedactMarkerNoParserDecodedCredentialSurvives(t *testing.T) {
	t.Parallel()

	listed := append(append([]string{}, yarnrcCredentialKeys...), oracleURLKeys...)
	rng := rand.New(rand.NewSource(int64(envInt("ESHU_8833_YAML_XPARSER_SEED", 88331))))
	count := envInt("ESHU_8833_YAML_XPARSER_N", 6000)
	bodies := make([]string, count)
	var lines bytes.Buffer
	for i := range bodies {
		bodies[i] = randomOracleBody(rng)
		line, err := json.Marshal(map[string]string{"body": bodies[i]})
		if err != nil {
			t.Fatal(err)
		}
		lines.Write(line)
		lines.WriteByte('\n')
	}
	file := filepath.Join(t.TempDir(), "bodies.jsonl")
	if err := os.WriteFile(file, lines.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	external := map[string][]map[string]bool{}
	if tokens, ok := externalTokens(t, "js-yaml", []string{"node", "-e", jsYAMLScript, "--"}, file, count); ok {
		external["js-yaml"] = tokens
	}
	if tokens, ok := externalTokens(t, "PyYAML", []string{"python3", "-c", pyYAMLScript}, file, count); ok {
		external["PyYAML"] = tokens
	}
	if len(external) == 0 {
		t.Skip("neither node with js-yaml nor python3 with PyYAML is installed")
	}
	checked := 0
	for i, body := range bodies {
		out, _, err := redactMarkerWithURLKeys(body, yarnrcCredentialKeys, oracleURLKeys)
		if err != nil {
			if !errors.Is(err, errYAMLIndirection) {
				t.Fatalf("unexpected err %v for %q", err, body)
			}
			continue
		}
		own, _ := yamlTokensUnderListedKey(body, listed)
		sets := []map[string]bool{own}
		for _, tokens := range external {
			sets = append(sets, tokens[i])
		}
		for _, set := range sets {
			for token := range set {
				checked++
				if strings.Contains(out, token) {
					t.Fatalf("a decoded credential %s was stored:\n in  %q\n out %q", token, body, out)
				}
			}
		}
	}
	t.Logf("parsers %d external, decoded tokens checked %d", len(external), checked)
}
