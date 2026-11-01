// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeBinaryName is the argv[0] base name that turns this test binary into a
// stand-in for the pinned whisper-cli executable. The engine execs a symlink
// with this name; TestMain sees the name and runs fakeWhisper instead of the
// test suite.
const fakeBinaryName = "whisper-cli"

// canaryTranscript is sensitive-looking text the fake engine "hears". It must
// never appear in persisted facts, error strings, span attributes, or logs.
const (
	canaryPassword = "hunter2-canary"
	canaryEmail    = "alice.canary@example.com"
	canaryStderr   = "STDERR_CANARY_TRANSCRIPT_TEXT"
)

func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == fakeBinaryName {
		os.Exit(fakeWhisper(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeWhisper emulates whisper-cli. The model file content selects the
// behavior, so the pinned model digest also pins the fake's mode.
func fakeWhisper(args []string) int {
	if len(os.Environ()) != 0 {
		fmt.Fprintln(os.Stderr, "environment was not scrubbed")
		return 65
	}
	modelPath := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-m" {
			modelPath = args[i+1]
		}
	}
	want := expectedArgs(modelPath, "en", 4)
	if !slices.Equal(args, want) {
		fmt.Fprintf(os.Stderr, "unexpected args %q\n", args)
		return 64
	}
	model, err := os.ReadFile(modelPath)
	if err != nil {
		return 70
	}
	mode := strings.TrimSpace(strings.TrimPrefix(string(model), "mode="))
	if mode == "fail-if-called" {
		return 99
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil || !isCanonicalWAV(stdin) {
		fmt.Fprintln(os.Stderr, "stdin is not 16 kHz mono 16-bit WAV")
		return 66
	}
	switch mode {
	case "success":
		fmt.Fprint(os.Stdout, successJSON)
	case "control":
		fmt.Fprint(os.Stdout, "{\"transcription\": [{\"offsets\": {\"from\": 0, \"to\": 900}, \"text\": \" Page\tthe\x01 on-call.\"}]}")
	case "nospeech":
		fmt.Fprint(os.Stdout, `{"result": {"language": "en"}, "transcription": [{"offsets": {"from": 0, "to": 1000}, "text": " [BLANK_AUDIO]"}, {"offsets": {"from": 1000, "to": 2000}, "text": " (silence)"}]}`)
	case "empty":
		fmt.Fprint(os.Stdout, `{"transcription": []}`)
	case "missing":
		fmt.Fprint(os.Stdout, `{"result": {"language": "en"}}`)
	case "badjson":
		fmt.Fprint(os.Stdout, `{"transcription": [{"text": " my password is `+canaryPassword)
	case "hostile":
		fmt.Fprint(os.Stdout, `{"transcription": [{"offsets": {"from": 900, "to": 100}, "text": " `+canaryPassword+`"}]}`)
	case "long":
		fmt.Fprintf(os.Stdout, `{"transcription": [{"offsets": {"from": 0, "to": 100}, "text": "%s"}]}`, strings.Repeat("a", 5000))
	case "many":
		var b strings.Builder
		b.WriteString(`{"transcription": [`)
		for i := range 50 {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"offsets": {"from": %d, "to": %d}, "text": " word"}`, i*10, i*10+10)
		}
		b.WriteString("]}")
		fmt.Fprint(os.Stdout, b.String())
	case "sleep":
		time.Sleep(time.Minute)
	case "flood":
		chunk := bytes.Repeat([]byte("x"), 64<<10)
		for range 1024 {
			if _, err := os.Stdout.Write(chunk); err != nil {
				return 0
			}
		}
	case "stderrflood":
		chunk := bytes.Repeat([]byte(canaryStderr), 4096)
		for range 1024 {
			if _, err := os.Stderr.Write(chunk); err != nil {
				return 0
			}
		}
		fmt.Fprint(os.Stdout, successJSON)
	case "exit":
		fmt.Fprintln(os.Stderr, canaryStderr)
		return 3
	case "memory":
		// 8 GiB of address space with one touched page: under the cap the
		// mmap fails; without the cap only one page is committed, so the
		// run succeeds and the test detects the missing cap.
		hog := make([]byte, 8<<30)
		hog[len(hog)-1] = 1
		fmt.Fprint(os.Stdout, successJSON)
	default:
		return 71
	}
	return 0
}

const successJSON = `{
	"systeminfo": "AVX = 0 | NEON = 1",
	"params": {"model": "/models/ggml-base.en.bin", "language": "en", "translate": false},
	"result": {"language": "en"},
	"transcription": [
		{
			"timestamps": {"from": "00:00:00,000", "to": "00:00:01,500"},
			"offsets": {"from": 0, "to": 1500},
			"text": " Restart the payments worker.",
			"tokens": [
				{"text": "[_BEG_]", "id": 50363, "p": 0.5},
				{"text": " Restart", "id": 1, "p": 0.9},
				{"text": " the", "id": 2, "p": 0.8},
				{"text": "[_TT_75]", "id": 50438, "p": 0.1}
			]
		},
		{
			"offsets": {"from": 1500, "to": 3000},
			"text": " My password is ` + canaryPassword + `, mail ` + canaryEmail + `",
			"tokens": [{"text": " My", "id": 3, "p": 0.6}]
		},
		{
			"offsets": {"from": 3000, "to": 3500},
			"text": " [BLANK_AUDIO]"
		}
	]
}`

func isCanonicalWAV(body []byte) bool {
	if len(body) < 44 || string(body[0:4]) != "RIFF" || string(body[8:12]) != "WAVE" {
		return false
	}
	channels := binary.LittleEndian.Uint16(body[22:24])
	rate := binary.LittleEndian.Uint32(body[24:28])
	bits := binary.LittleEndian.Uint16(body[34:36])
	return channels == 1 && rate == 16000 && bits == 16
}

// newFakeEngine builds an Engine whose binary is a symlink to this test
// binary and whose model file selects the fake mode.
func newFakeEngine(t *testing.T, mode string, limits Limits) *Engine {
	t.Helper()
	cfg := fakeConfig(t, mode)
	cfg.Limits = limits
	engine, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return engine
}

func fakeConfig(t *testing.T, mode string) Config {
	t.Helper()
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	binary := filepath.Join(dir, fakeBinaryName)
	if err := os.Symlink(self, binary); err != nil {
		t.Fatalf("symlink fake binary: %v", err)
	}
	model := filepath.Join(dir, "ggml-fake.bin")
	if err := os.WriteFile(model, []byte("mode="+mode), 0o600); err != nil {
		t.Fatalf("write fake model: %v", err)
	}
	return Config{
		BinaryPath:    binary,
		BinarySHA256:  fileDigest(t, self),
		ModelPath:     model,
		ModelSHA256:   fileDigest(t, model),
		EngineVersion: "v1.9.5",
	}
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// encodeWAV builds a PCM WAV body with a deterministic waveform.
func encodeWAV(t *testing.T, sampleRate, channels, bits int, frames int) []byte {
	t.Helper()
	blockAlign := channels * bits / 8
	dataSize := frames * blockAlign
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*blockAlign))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(bits))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(dataSize))
	for i := range frames {
		for c := range channels {
			value := int32((i*37+c*11)%2000 - 1000)
			switch bits {
			case 8:
				buf.WriteByte(byte(value/8 + 128))
			case 16:
				_ = binary.Write(&buf, binary.LittleEndian, int16(value*8))
			case 24:
				v := value * 2048
				buf.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16)})
			case 32:
				_ = binary.Write(&buf, binary.LittleEndian, value*65536)
			}
		}
	}
	return buf.Bytes()
}
