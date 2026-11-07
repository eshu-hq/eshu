// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSonameFixture builds a minimal ELF64 shared object whose DT_SONAME
// is soname and returns its path plus hex SHA-256. It exercises the
// production debug/elf derivation path without vendoring a real library.
func writeSonameFixture(t *testing.T, dir, name, soname string) (string, string) {
	t.Helper()
	dynstr := append([]byte{0}, append([]byte(soname), 0)...)
	dyn := make([]byte, 32)
	binary.LittleEndian.PutUint64(dyn[0:8], 14)
	binary.LittleEndian.PutUint64(dyn[8:16], 1)
	shstr := []byte{0}
	offDynstrName := len(shstr)
	shstr = append(shstr, []byte(".dynstr\x00")...)
	offDynName := len(shstr)
	shstr = append(shstr, []byte(".dynamic\x00")...)
	offShName := len(shstr)
	shstr = append(shstr, []byte(".shstrtab\x00")...)
	offDynstr, offDyn := 64, 64+len(dynstr)
	offShstr := offDyn + len(dyn)
	shoff := offShstr + len(shstr)
	for shoff%8 != 0 {
		shoff++
	}
	buf := make([]byte, shoff+4*64)
	copy(buf[0:4], []byte{0x7f, 'E', 'L', 'F'})
	buf[4], buf[5], buf[6] = 2, 1, 1
	binary.LittleEndian.PutUint16(buf[16:18], 3)
	binary.LittleEndian.PutUint16(buf[18:20], 62)
	binary.LittleEndian.PutUint32(buf[20:24], 1)
	binary.LittleEndian.PutUint64(buf[40:48], uint64(shoff))
	buf[52], buf[58], buf[60], buf[62] = 64, 64, 4, 3
	copy(buf[offDynstr:], dynstr)
	copy(buf[offDyn:], dyn)
	copy(buf[offShstr:], shstr)
	writeSH := func(i int, name uint32, typ uint32, flags uint64, off uint64, size uint64, link uint32) {
		b := buf[shoff+i*64:]
		binary.LittleEndian.PutUint32(b[0:4], name)
		binary.LittleEndian.PutUint32(b[4:8], typ)
		binary.LittleEndian.PutUint64(b[8:16], flags)
		binary.LittleEndian.PutUint64(b[24:32], off)
		binary.LittleEndian.PutUint64(b[32:40], size)
		binary.LittleEndian.PutUint32(b[40:44], link)
		binary.LittleEndian.PutUint64(b[48:56], 1)
		if typ == 6 {
			binary.LittleEndian.PutUint64(b[48:56], 8)
			binary.LittleEndian.PutUint64(b[56:64], 16)
		}
	}
	writeSH(1, uint32(offDynstrName), 3, 2, uint64(offDynstr), uint64(len(dynstr)), 0)
	writeSH(2, uint32(offDynName), 6, 3, uint64(offDyn), uint64(len(dyn)), 1)
	writeSH(3, uint32(offShName), 3, 0, uint64(offShstr), uint64(len(shstr)), 0)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write soname fixture: %v", err)
	}
	sum := sha256.Sum256(buf)
	return path, hex.EncodeToString(sum[:])
}

// TestSeedEnginePayloadPlantsSonameSymlink proves the composed seed derives
// the ELF DT_SONAME from verified lib bytes and plants a resolving relative
// soname link beside the real file.
func TestSeedEnginePayloadPlantsSonameSymlink(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	real, soname := "libsonametest.so.5.0.5", "libsonametest.so.5"
	libSrc, libSHA := writeSonameFixture(t, src, real, soname)
	binSrc, binSHA := fixtureEngineFile(t, src, "tesseract", []byte("fake-tesseract-binary"))
	modelSrc, modelSHA := fixtureEngineFile(t, src, "eng.traineddata", []byte("fake-model-bytes"))
	payload := engineSeedPayload{engine: "tesseract", version: "5.5.0", platform: "linux/test", files: []engineSeedFile{
		{source: binSrc, target: "bin/tesseract", sha256: binSHA, executable: true},
		{source: libSrc, target: "lib/" + real, sha256: libSHA},
		{source: modelSrc, target: "tessdata/eng.traineddata", sha256: modelSHA},
	}}
	dest := filepath.Join(t.TempDir(), "dest")
	if err := seedEnginePayload(dest, payload); err != nil {
		t.Fatalf("seedEnginePayload: %v", err)
	}
	link := filepath.Join(dest, "engine", "lib", soname)
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("seed left no soname link %q: %v", soname, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("seeded %q mode = %v, want a symlink", soname, fi.Mode())
	}
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink %q: %v", soname, err)
	}
	if got != real {
		t.Fatalf("soname link target = %q, want relative %q", got, real)
	}
	// #nosec G304 -- link joins the seeded engine lib dir under test.
	viaLink, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("read through soname link: %v", err)
	}
	// #nosec G304 -- source path is a test fixture created above.
	original, err := os.ReadFile(libSrc)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !bytes.Equal(viaLink, original) {
		t.Fatal("bytes through the soname link differ from the pinned source bytes")
	}
}

// writeMultiSonameFixture builds a minimal ELF64 shared object declaring
// two DT_SONAME entries. The seeder must fail closed: exactly one SONAME
// is the contract.
func writeMultiSonameFixture(t *testing.T, dir, name, first, second string) string {
	t.Helper()
	dynstr := []byte{0}
	off1 := len(dynstr)
	dynstr = append(dynstr, append([]byte(first), 0)...)
	off2 := len(dynstr)
	dynstr = append(dynstr, append([]byte(second), 0)...)
	dyn := make([]byte, 48)
	binary.LittleEndian.PutUint64(dyn[0:8], 14)
	binary.LittleEndian.PutUint64(dyn[8:16], uint64(off1))
	binary.LittleEndian.PutUint64(dyn[16:24], 14)
	binary.LittleEndian.PutUint64(dyn[24:32], uint64(off2))
	shstr := []byte{0}
	offDynstrName := len(shstr)
	shstr = append(shstr, []byte(".dynstr\x00")...)
	offDynName := len(shstr)
	shstr = append(shstr, []byte(".dynamic\x00")...)
	offShName := len(shstr)
	shstr = append(shstr, []byte(".shstrtab\x00")...)
	offDynstr, offDyn := 64, 64+len(dynstr)
	offShstr := offDyn + len(dyn)
	shoff := offShstr + len(shstr)
	for shoff%8 != 0 {
		shoff++
	}
	buf := make([]byte, shoff+4*64)
	copy(buf[0:4], []byte{0x7f, 'E', 'L', 'F'})
	buf[4], buf[5], buf[6] = 2, 1, 1
	binary.LittleEndian.PutUint16(buf[16:18], 3)
	binary.LittleEndian.PutUint16(buf[18:20], 62)
	binary.LittleEndian.PutUint32(buf[20:24], 1)
	binary.LittleEndian.PutUint64(buf[40:48], uint64(shoff))
	buf[52], buf[58], buf[60], buf[62] = 64, 64, 4, 3
	copy(buf[offDynstr:], dynstr)
	copy(buf[offDyn:], dyn)
	copy(buf[offShstr:], shstr)
	writeSH := func(i int, name uint32, typ uint32, flags uint64, off uint64, size uint64, link uint32) {
		b := buf[shoff+i*64:]
		binary.LittleEndian.PutUint32(b[0:4], name)
		binary.LittleEndian.PutUint32(b[4:8], typ)
		binary.LittleEndian.PutUint64(b[8:16], flags)
		binary.LittleEndian.PutUint64(b[24:32], off)
		binary.LittleEndian.PutUint64(b[32:40], size)
		binary.LittleEndian.PutUint32(b[40:44], link)
		binary.LittleEndian.PutUint64(b[48:56], 1)
		if typ == 6 {
			binary.LittleEndian.PutUint64(b[48:56], 8)
			binary.LittleEndian.PutUint64(b[56:64], 16)
		}
	}
	writeSH(1, uint32(offDynstrName), 3, 2, uint64(offDynstr), uint64(len(dynstr)), 0)
	writeSH(2, uint32(offDynName), 6, 3, uint64(offDyn), uint64(len(dyn)), 1)
	writeSH(3, uint32(offShName), 3, 0, uint64(offShstr), uint64(len(shstr)), 0)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write multi-soname fixture: %v", err)
	}
	return path
}

// TestPlantSonameSymlinkGuards proves every fail-closed branch of the
// soname derivation: unsafe names, collisions, malformed ELF, and
// multi-SONAME objects. Deleting any guard must turn its row red.
func TestPlantSonameSymlinkGuards(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		soname  string
		multi   bool
		prelink string
		wantErr string
	}{
		{name: "absolute soname", soname: "/lib/evil.so", wantErr: "unsafe SONAME"},
		{name: "parent traversal", soname: "../evil.so", wantErr: "unsafe SONAME"},
		{name: "nested path", soname: "sub/evil.so", wantErr: "unsafe SONAME"},
		{name: "dot soname", soname: ".", wantErr: "unsafe SONAME"},
		{name: "dotdot soname", soname: "..", wantErr: "unsafe SONAME"},
		{name: "multi soname", soname: "libmulti.so.1", multi: true, wantErr: "exactly one"},
		{name: "colliding regular file", soname: "libcollide.so.1", prelink: "file", wantErr: "collides"},
		{name: "colliding wrong symlink", soname: "libcollide.so.1", prelink: "symlink", wantErr: "collides"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			real := "libguard.so.9.9.9"
			var lib string
			if tc.multi {
				lib = filepath.Join(dir, real)
				src := writeMultiSonameFixture(t, dir, "src.so", "libmulti.so.1", "libmulti.so.2")
				// #nosec G304 -- source is a test fixture created above.
				raw, err := os.ReadFile(src)
				if err != nil {
					t.Fatalf("read multi-soname fixture: %v", err)
				}
				if err := os.WriteFile(lib, raw, 0o600); err != nil {
					t.Fatalf("stage multi-soname fixture: %v", err)
				}
			} else {
				var src string
				src, _ = writeSonameFixture(t, dir, "src.so", tc.soname)
				// #nosec G304 -- source is a test fixture created above.
				raw, err := os.ReadFile(src)
				if err != nil {
					t.Fatalf("read soname fixture: %v", err)
				}
				lib = filepath.Join(dir, real)
				if err := os.WriteFile(lib, raw, 0o600); err != nil {
					t.Fatalf("stage soname fixture: %v", err)
				}
			}
			switch tc.prelink {
			case "file":
				if err := os.WriteFile(filepath.Join(dir, "libcollide.so.1"), []byte("x"), 0o600); err != nil {
					t.Fatalf("stage colliding file: %v", err)
				}
			case "symlink":
				if err := os.Symlink("elsewhere.so", filepath.Join(dir, "libcollide.so.1")); err != nil {
					t.Fatalf("stage colliding symlink: %v", err)
				}
			}
			err := plantSonameSymlink(lib)
			if err == nil {
				t.Fatalf("plantSonameSymlink(%q): got nil error, want %q", tc.soname, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("plantSonameSymlink(%q) error = %q, want substring %q", tc.soname, err, tc.wantErr)
			}
		})
	}
}

// TestSeedEnginePayloadSonameReseed proves re-seeding is idempotent: the
// second run succeeds and leaves the identical relative link.
func TestSeedEnginePayloadSonameReseed(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	real, soname := "libsonametest.so.5.0.5", "libsonametest.so.5"
	libSrc, libSHA := writeSonameFixture(t, src, real, soname)
	binSrc, binSHA := fixtureEngineFile(t, src, "tesseract", []byte("fake-tesseract-binary"))
	modelSrc, modelSHA := fixtureEngineFile(t, src, "eng.traineddata", []byte("fake-model-bytes"))
	payload := engineSeedPayload{engine: "tesseract", version: "5.5.0", platform: "linux/test", files: []engineSeedFile{
		{source: binSrc, target: "bin/tesseract", sha256: binSHA, executable: true},
		{source: libSrc, target: "lib/" + real, sha256: libSHA},
		{source: modelSrc, target: "tessdata/eng.traineddata", sha256: modelSHA},
	}}
	dest := filepath.Join(t.TempDir(), "dest")
	for i := 0; i < 2; i++ {
		if err := seedEnginePayload(dest, payload); err != nil {
			t.Fatalf("seedEnginePayload run %d: %v", i, err)
		}
	}
	link := filepath.Join(dest, "engine", "lib", soname)
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink after reseed: %v", err)
	}
	if got != real {
		t.Fatalf("soname link target after reseed = %q, want %q", got, real)
	}
	// The re-seed retry loosens the binary to 0600 mid-copy; the final
	// mode must be re-asserted or the engine loses its execute bit.
	binInfo, err := os.Stat(filepath.Join(dest, "engine", "bin", "tesseract"))
	if err != nil {
		t.Fatalf("stat reseeded binary: %v", err)
	}
	if perm := binInfo.Mode().Perm(); perm != 0o555 {
		t.Fatalf("reseeded binary mode = %o, want 555", perm)
	}
}

// TestSeedEnginePayloadIgnoresNonELF proves non-ELF lib payload entries
// pass through without a link and without an error.
func TestSeedEnginePayloadIgnoresNonELF(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	binSrc, binSHA := fixtureEngineFile(t, src, "tesseract", []byte("fake-tesseract-binary"))
	modelSrc, modelSHA := fixtureEngineFile(t, src, "eng.traineddata", []byte("fake-model-bytes"))
	blobSrc, blobSHA := fixtureEngineFile(t, src, "opaque.dat", []byte("not-an-elf-object"))
	payload := engineSeedPayload{engine: "tesseract", version: "5.5.0", platform: "linux/test", files: []engineSeedFile{
		{source: binSrc, target: "bin/tesseract", sha256: binSHA, executable: true},
		{source: blobSrc, target: "lib/opaque.dat", sha256: blobSHA},
		{source: modelSrc, target: "tessdata/eng.traineddata", sha256: modelSHA},
	}}
	dest := filepath.Join(t.TempDir(), "dest")
	if err := seedEnginePayload(dest, payload); err != nil {
		t.Fatalf("seedEnginePayload with non-ELF lib entry: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dest, "engine", "lib"))
	if err != nil {
		t.Fatalf("read lib dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "opaque.dat" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("lib dir = %v, want only the opaque payload file", names)
	}
}

// TestSeedEnginePayloadSweepsStaleSonameLinks proves a re-seed with a new
// payload version removes the previous version's soname links: without the
// sweep the stale link shadows the fresh payload and the loader resolves
// the old library.
func TestSeedEnginePayloadSweepsStaleSonameLinks(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	oldReal, oldSoname := "libsonametest.so.5.0.5", "libsonametest.so.5"
	newReal, newSoname := "libsonametest.so.6.0.0", "libsonametest.so.6"
	oldSrc, oldSHA := writeSonameFixture(t, src, oldReal, oldSoname)
	newSrc, newSHA := writeSonameFixture(t, src, newReal, newSoname)
	binSrc, binSHA := fixtureEngineFile(t, src, "tesseract", []byte("fake-tesseract-binary"))
	modelSrc, modelSHA := fixtureEngineFile(t, src, "eng.traineddata", []byte("fake-model-bytes"))
	files := func(libSrc, libReal, libSHA string) []engineSeedFile {
		return []engineSeedFile{
			{source: binSrc, target: "bin/tesseract", sha256: binSHA, executable: true},
			{source: libSrc, target: "lib/" + libReal, sha256: libSHA},
			{source: modelSrc, target: "tessdata/eng.traineddata", sha256: modelSHA},
		}
	}
	dest := filepath.Join(t.TempDir(), "dest")
	if err := seedEnginePayload(dest, engineSeedPayload{engine: "tesseract", version: "5.5.0", platform: "linux/test", files: files(oldSrc, oldReal, oldSHA)}); err != nil {
		t.Fatalf("seedEnginePayload v5: %v", err)
	}
	if err := seedEnginePayload(dest, engineSeedPayload{engine: "tesseract", version: "6.0.0", platform: "linux/test", files: files(newSrc, newReal, newSHA)}); err != nil {
		t.Fatalf("seedEnginePayload v6: %v", err)
	}
	libDir := filepath.Join(dest, "engine", "lib")
	if _, err := os.Lstat(filepath.Join(libDir, oldSoname)); !os.IsNotExist(err) {
		t.Fatalf("stale soname link %q survives the version-change reseed (err=%v)", oldSoname, err)
	}
	got, err := os.Readlink(filepath.Join(libDir, newSoname))
	if err != nil {
		t.Fatalf("readlink %q after version-change reseed: %v", newSoname, err)
	}
	if got != newReal {
		t.Fatalf("soname link target = %q, want relative %q", got, newReal)
	}
}
