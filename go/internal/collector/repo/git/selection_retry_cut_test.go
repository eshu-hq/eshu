// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build unix

package git

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/deadline"
)

// cutShape says where, and how, the first fetch response is cut off.
type cutShape struct {
	name string
	// chunked sends the response with chunked transfer encoding; otherwise it
	// declares a Content-Length it never completes.
	chunked bool
	// inHeader cuts a few bytes into the packfile section; otherwise the cut is
	// at 70% of the pack.
	inHeader bool
}

// cutPackGitServer is a real smart-HTTP remote that, for its first fetch
// command, sends part of the response and drops the connection: the
// mid-transfer drop that production sees as "transfer closed" with git's
// pack follow-on lines after it. The retry must treat it as the transport
// failure it is, not as a pack integrity error (#8091 review F1).
type cutPackGitServer struct {
	URL    string
	Commit string
	posts  atomic.Int32
}

func startCutPackGitHTTP(t *testing.T, shape cutShape) *cutPackGitServer {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git binary not found")
	}
	execPathOut, err := exec.Command(gitBin, "--exec-path").Output()
	if err != nil {
		t.Skipf("git --exec-path: %v", err)
	}
	backendPath := filepath.Join(strings.TrimSpace(string(execPathOut)), "git-http-backend")

	work := t.TempDir()
	runGitHere(t, work, "init", "-q", "-b", "main")
	for _, name := range []string{"first", "second"} {
		blob := make([]byte, 400*1024)
		if _, err := rand.Read(blob); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, "blob-"+name+".bin"), blob, 0o644); err != nil {
			t.Fatal(err)
		}
		runGitHere(t, work, "add", "-A")
		runGitHere(t, work, "-c", "user.name=t", "-c", "user.email=t@example.test", "-c", "commit.gpgsign=false",
			"commit", "-q", "-m", name)
	}
	root := t.TempDir()
	runGitHere(t, root, "clone", "-q", "--bare", work, filepath.Join(root, "x.git"))
	shaOut, err := exec.Command("git", "-C", work, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}

	server := &cutPackGitServer{Commit: strings.TrimSpace(string(shaOut))}
	backend := &cgi.Handler{
		Path: backendPath,
		Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request []byte
		if r.Method == http.MethodPost {
			request, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(request))
		}
		if !bytes.Contains(request, []byte("command=fetch")) || server.posts.Add(1) > 1 {
			backend.ServeHTTP(w, r)
			return
		}
		recorded := httptest.NewRecorder()
		backend.ServeHTTP(recorded, r)
		body := recorded.Body.Bytes()
		packAt := bytes.Index(body, []byte("packfile\n"))
		if packAt < 0 {
			t.Errorf("fetch response has no packfile section (%d bytes)", len(body))
			backend.ServeHTTP(w, r)
			return
		}
		cut := packAt + len("packfile\n") + 6
		if !shape.inHeader {
			cut = packAt + (len(body)-packAt)*7/10
		}
		conn, buf, hijackErr := w.(http.Hijacker).Hijack()
		if hijackErr != nil {
			t.Errorf("hijack: %v", hijackErr)
			return
		}
		defer conn.Close()
		contentType := recorded.Header().Get("Content-Type")
		fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: %s\r\nConnection: close\r\n", contentType)
		if shape.chunked {
			fmt.Fprintf(buf, "Transfer-Encoding: chunked\r\n\r\n%x\r\n", cut)
			buf.Write(body[:cut])
			buf.WriteString("\r\n")
		} else {
			fmt.Fprintf(buf, "Content-Length: %d\r\n\r\n", len(body))
			buf.Write(body[:cut])
		}
		buf.Flush()
	}))
	t.Cleanup(httpServer.Close)
	server.URL = httpServer.URL + "/x.git"
	return server
}

// TestRealGitFetchRetriesAMidTransferDrop is the F1 proof: a connection cut
// during the pack is a transport failure however git words its follow-on lines.
// Each shape the reviewer reproduced must recover on the second attempt.
func TestRealGitFetchRetriesAMidTransferDrop(t *testing.T) {
	shapes := []cutShape{
		{name: "content-length cut in the pack header", inHeader: true},
		{name: "content-length cut at 70 percent of the pack"},
		{name: "chunked cut at 70 percent of the pack", chunked: true},
		{name: "chunked cut in the pack header", chunked: true, inHeader: true},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			isolateGitEnv(t)
			server := startCutPackGitHTTP(t, shape)
			reposDir := t.TempDir()
			repoPath := filepath.Join(reposDir, "github", "org", "repo-a")
			newRealRepo(t, repoPath, server.URL)
			limits := deadline.Limits{
				Resolve: time.Minute, Fetch: 30 * time.Second, Clone: time.Minute,
				RetryAttempts: 2, RetryBackoff: 50 * time.Millisecond,
			}
			config := realGitConfig(reposDir, limits)

			err := gitFetchBranch(context.Background(), config, repoPath, "main", "", nil, gitSyncLogEvent{}, "")
			if err != nil {
				t.Fatalf("gitFetchBranch() error = %v, want the retry to recover from the dropped transfer", err)
			}
			got, outErr := exec.Command("git", "-C", repoPath, "rev-parse", "refs/remotes/origin/main").Output()
			if outErr != nil || strings.TrimSpace(string(got)) != server.Commit {
				t.Fatalf("origin/main = %q (%v), want %q", strings.TrimSpace(string(got)), outErr, server.Commit)
			}
			if posts := server.posts.Load(); posts != 2 {
				t.Fatalf("fetch POSTs = %d, want 2: the dropped one and the retry", posts)
			}
		})
	}
}
