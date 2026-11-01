// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain doubles the compiled test binary as a fake tesseract executable.
// The engine runs its binary with a scrubbed environment and a fixed argument
// vector, so the fake recognizes itself by that vector and picks its behavior
// from a FAKEOCR:<mode> token embedded in the image bytes on stdin.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		_, _ = os.Stdout.WriteString("tesseract 5.5.0\n leptonica-1.84.1\n")
		os.Exit(0)
	}
	if len(os.Args) > 2 && os.Args[1] == "stdin" && os.Args[2] == "stdout" {
		os.Exit(runFakeTesseract(os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// fakeTSVHeader is the tesseract 5.x TSV header row.
const fakeTSVHeader = "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n"

// canaryPassword and canaryEmail are synthetic PII canaries the fake emits.
// They must never reach persisted section content, errors, or logs.
const (
	canaryPassword = "Hunter2CanarySecret"
	canaryEmail    = "alice.canary@example.com"
)

func runFakeTesseract(stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	body, _ := io.ReadAll(stdin)
	mode := "ok"
	if idx := bytes.Index(body, []byte("FAKEOCR:")); idx >= 0 {
		rest := body[idx+len("FAKEOCR:"):]
		end := bytes.IndexAny(rest, " \n\x00")
		if end < 0 {
			end = len(rest)
		}
		mode = string(rest[:end])
	}
	waitForFakeGate(body)
	_, _ = fmt.Fprintln(stderr, "Estimating resolution as 264")
	switch mode {
	case "ok":
		_, _ = io.WriteString(stdout, fakeTSVHeader+
			"1\t1\t0\t0\t0\t0\t0\t0\t200\t100\t-1\t\n"+
			"4\t1\t1\t1\t1\t0\t10\t10\t180\t20\t-1\t\n"+
			"5\t1\t1\t1\t1\t1\t10\t10\t60\t20\t96.5\tDeploy\n"+
			"5\t1\t1\t1\t1\t2\t80\t10\t40\t20\t95.5\tthe\n"+
			"5\t1\t1\t1\t1\t3\t130\t10\t60\t20\t94.0\tCheckout\n"+
			"5\t1\t1\t1\t2\t1\t10\t40\t80\t20\t93.0\tpassword:\n"+
			"5\t1\t1\t1\t2\t2\t100\t40\t90\t20\t91.0\t"+canaryPassword+"\n"+
			"5\t1\t1\t1\t3\t1\t10\t70\t60\t20\t92.0\tContact\n"+
			"5\t1\t1\t1\t3\t2\t80\t70\t110\t20\t88.0\t"+canaryEmail+"\n"+
			"5\t1\t1\t1\t3\t3\t195\t70\t5\t20\t-1\t \n")
		return 0
	case "args":
		var builder strings.Builder
		builder.WriteString(fakeTSVHeader)
		for i, arg := range os.Args[1:] {
			fmt.Fprintf(&builder, "5\t1\t1\t1\t%d\t1\t1\t1\t1\t1\t90\t%s\n", i+1, arg)
		}
		_, _ = io.WriteString(stdout, builder.String())
		return 0
	case "formlayout":
		// A form layout: the label and its value land on separate tesseract
		// lines, so they become separate regions.
		_, _ = io.WriteString(stdout, fakeTSVHeader+
			"5\t1\t1\t1\t1\t1\t10\t10\t80\t20\t93.0\tpassword:\n"+
			"5\t1\t1\t1\t2\t1\t10\t40\t90\t20\t91.0\t"+canaryPassword+"\n")
		return 0
	case "hold":
		held := make([]byte, 32<<20)
		for i := range held {
			held[i] = 1
		}
		time.Sleep(150 * time.Millisecond)
		_, _ = io.WriteString(stdout, fakeTSVHeader)
		return int(held[len(held)-1]) - 1
	case "limits":
		// RLIMIT_AS lands just after exec, so poll until it is visible.
		limit := "unlimited"
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if limit = addressSpaceSoftLimit(); limit != "unlimited" {
				break
			}
		}
		_, _ = io.WriteString(stdout, fakeTSVHeader+"5\t1\t1\t1\t1\t1\t1\t1\t1\t1\t90\tAS="+limit+"\n")
		return 0
	case "selfkill":
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		time.Sleep(time.Second)
		return 0
	case "empty":
		_, _ = io.WriteString(stdout, fakeTSVHeader+"1\t1\t0\t0\t0\t0\t0\t0\t200\t100\t-1\t\n")
		return 0
	case "sleep":
		time.Sleep(time.Minute)
		return 0
	case "flood":
		chunk := bytes.Repeat([]byte("5\t1\t1\t1\t1\t1\t1\t1\t1\t1\t90\tflood\n"), 4096)
		for i := 0; i < 1024; i++ {
			if _, err := stdout.Write(chunk); err != nil {
				return 1
			}
		}
		return 0
	case "stderrflood":
		chunk := bytes.Repeat([]byte("noise "+canaryPassword+"\n"), 4096)
		for i := 0; i < 1024; i++ {
			if _, err := stderr.Write(chunk); err != nil {
				return 1
			}
		}
		return 0
	case "exit3":
		_, _ = fmt.Fprintln(stderr, "Error: leaked "+canaryPassword)
		_, _ = io.WriteString(stdout, fakeTSVHeader+"5\t1\t1\t1\t1\t1\t1\t1\t1\t1\t90\t"+canaryPassword+"\n")
		return 3
	case "garbage":
		_, _ = io.WriteString(stdout, "not a tsv "+canaryPassword+"\n")
		return 0
	case "badrow":
		_, _ = io.WriteString(stdout, fakeTSVHeader+"5\t1\t1\t"+canaryPassword+"\n")
		return 0
	case "manylines":
		var builder strings.Builder
		builder.WriteString(fakeTSVHeader)
		for line := 1; line <= 40; line++ {
			fmt.Fprintf(&builder, "5\t1\t1\t1\t%d\t1\t1\t%d\t10\t2\t90\tline%d\n", line, line*2, line)
		}
		_, _ = io.WriteString(stdout, builder.String())
		return 0
	case "env":
		var builder strings.Builder
		builder.WriteString(fakeTSVHeader)
		for i, kv := range os.Environ() {
			fmt.Fprintf(&builder, "5\t1\t1\t1\t%d\t1\t1\t1\t1\t1\t90\t%s\n", i+1, strings.ReplaceAll(kv, "\t", " "))
		}
		wd, _ := os.Getwd()
		fmt.Fprintf(&builder, "5\t1\t1\t1\t999\t1\t1\t1\t1\t1\t90\tCWD=%s\n", wd)
		_, _ = io.WriteString(stdout, builder.String())
		return 0
	default:
		return 2
	}
}

// fakeGateToken marks a gated run. The fake blocks after reading stdin until
// the file named after the token exists, so a test can keep the child alive
// until the parent's /proc sampler has read it. A zombie has no VmHWM, so a
// child that exits first leaves the run with no peak-RSS sample.
const fakeGateToken = "FAKEGATE:"

// fakeGateTimeout bounds the wait so a lost handshake fails the calling
// test instead of hanging it.
const fakeGateTimeout = 10 * time.Second

// waitForFakeGate polls for the gate file named in body, if any.
func waitForFakeGate(body []byte) {
	idx := bytes.Index(body, []byte(fakeGateToken))
	if idx < 0 {
		return
	}
	rest := body[idx+len(fakeGateToken):]
	end := bytes.IndexAny(rest, " \n\x00")
	if end < 0 {
		end = len(rest)
	}
	path := string(rest[:end])
	for deadline := time.Now().Add(fakeGateTimeout); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
}

// addressSpaceSoftLimit returns the "Max address space" soft limit from
// /proc/self/limits, or "unlimited" when absent or unreadable.
func addressSpaceSoftLimit() string {
	body, err := os.ReadFile("/proc/self/limits")
	if err != nil {
		return "unlimited"
	}
	for _, line := range strings.Split(string(body), "\n") {
		if rest, ok := strings.CutPrefix(line, "Max address space"); ok {
			if fields := strings.Fields(rest); len(fields) > 0 {
				return fields[0]
			}
		}
	}
	return "unlimited"
}

// fixture holds a fake engine install: the test binary as executable plus a
// synthetic traineddata file, with their pinned digests.
type fixture struct {
	binary       string
	binaryDigest string
	tessdata     string
	modelDigest  string
	libraries    []PinnedFile
	scratch      string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	tessdata := t.TempDir()
	model := []byte("synthetic eng traineddata for tests")
	if err := os.WriteFile(filepath.Join(tessdata, "eng.traineddata"), model, 0o600); err != nil {
		t.Fatalf("write model: %v", err)
	}
	libDir := t.TempDir()
	libraries := []PinnedFile{}
	for _, name := range []string{"libtesseract.so.5", "libleptonica.so.6"} {
		body := []byte("synthetic " + name)
		path := filepath.Join(libDir, name)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		libraries = append(libraries, PinnedFile{Path: path, SHA256: sha256Hex(body)})
	}
	return fixture{
		binary:       binary,
		binaryDigest: fileDigest(t, binary),
		tessdata:     tessdata,
		modelDigest:  sha256Hex(model),
		libraries:    libraries,
		scratch:      t.TempDir(),
	}
}

func (f fixture) config() Config {
	return Config{
		BinaryPath:    f.binary,
		BinarySHA256:  f.binaryDigest,
		TessdataDir:   f.tessdata,
		ModelSHA256:   f.modelDigest,
		Libraries:     append([]PinnedFile(nil), f.libraries...),
		EngineVersion: "5.5.0",
		ScratchDir:    f.scratch,
		// The fake is this Go test binary; under -race its runtime maps
		// terabytes of shadow address space, so tests that do not exercise
		// RLIMIT_AS lift it out of the way.
		MaxAddressSpaceBytes: 1 << 62,
	}
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return sha256Hex(body)
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// fakePNG returns a valid PNG header for a width x height image followed by
// the FAKEOCR mode token after IEND, which image.DecodeConfig never reads.
func fakePNG(t *testing.T, width, height int, mode string) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, width, height))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	buf.WriteString("FAKEOCR:" + mode + " ")
	return buf.Bytes()
}

// fakeWebP returns a minimal lossless (VP8L) WebP container header for a
// width x height image followed by the FAKEOCR mode token.
func fakeWebP(width, height int, mode string) []byte {
	payload := []byte{0x2f}
	bits := uint32(width-1) | uint32(height-1)<<14
	payload = append(payload, byte(bits), byte(bits>>8), byte(bits>>16), byte(bits>>24))
	chunk := append([]byte("VP8L"), le32(uint32(len(payload)))...)
	chunk = append(chunk, payload...)
	if len(payload)%2 == 1 {
		chunk = append(chunk, 0)
	}
	out := append([]byte("RIFF"), le32(uint32(4+len(chunk)))...)
	out = append(out, []byte("WEBP")...)
	out = append(out, chunk...)
	return append(out, []byte("FAKEOCR:"+mode+" ")...)
}

func le32(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}
