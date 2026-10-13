// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"errors"
	"testing"
	"time"
)

// TestClassifyClosedTable is the closed retry-class table of #8091: one row
// per retryable libcurl, TLS, SSH, and deadline signal, and one row per
// non-retryable class. A message outside the table is never retried. The
// "ssh" rows are matched on documented OpenSSH message text and were not
// exercised against a real SSH remote.
func TestClassifyClosedTable(t *testing.T) {
	deadlineErr := &Error{Operation: Fetch, Command: "fetch", Limit: 90 * time.Second, Elapsed: 90 * time.Second}
	rows := []struct {
		name      string
		err       error
		stderr    string
		wantClass string
		wantRetry bool
	}{
		// Retryable.
		{"deadline kill", deadlineErr, "", ClassDeadlineExceeded, true},
		{"deadline wins over a resolver line", deadlineErr, "Could not resolve host: github.com", ClassDeadlineExceeded, true},
		{"could not resolve host", nil, "fatal: unable to access 'https://github.com/o/r.git/': Could not resolve host: github.com", ClassDNSResolve, true},
		{"resolving timed out", nil, "fatal: unable to access 'https://github.com/o/r.git/': Resolving timed out after 300001 milliseconds", ClassDNSResolve, true},
		{"failed to connect", nil, "fatal: unable to access 'https://github.com/o/r.git/': Failed to connect to github.com port 443 after 130 ms: Couldn't connect to server", ClassConnect, true},
		{"connection timed out", nil, "fatal: unable to access 'https://github.com/o/r.git/': Connection timed out after 300001 milliseconds", ClassConnect, true},
		{"connection reset", nil, "fatal: unable to access 'https://github.com/o/r.git/': Connection reset by peer", ClassConnectionReset, true},
		{"recv failure", nil, "error: RPC failed; curl 56 Recv failure: Connection reset by peer", ClassConnectionReset, true},
		{"empty reply from server", nil, "fatal: unable to access 'https://github.com/o/r.git/': Empty reply from server", ClassConnectionReset, true},
		{"http2 framing", nil, "error: RPC failed; curl 16 Error in the HTTP2 framing layer", ClassHTTP2Framing, true},
		{"transfer closed", nil, "error: RPC failed; curl 18 transfer closed with outstanding read data remaining", ClassConnectionReset, true},
		{"ssl_connect", nil, "fatal: unable to access 'https://github.com/o/r.git/': OpenSSL SSL_connect: Connection reset by peer in connection to github.com:443", ClassTLSHandshake, true},
		{"ssl_error_syscall", nil, "fatal: unable to access 'https://github.com/o/r.git/': SSL_ERROR_SYSCALL in connection to github.com:443", ClassTLSHandshake, true},
		{"tls connect error", nil, "fatal: unable to access 'https://github.com/o/r.git/': TLS connect error: error:0A000126:SSL routines::unexpected eof while reading", ClassTLSHandshake, true},
		{"hung up paired with a transport line", nil, "error: RPC failed; curl 56 Recv failure: Connection reset by peer\nfatal: the remote end hung up unexpectedly", ClassConnectionReset, true},
		{"ssh could not resolve", nil, "ssh: Could not resolve hostname github.com: Temporary failure in name resolution\nfatal: Could not read from remote repository.", ClassDNSResolve, true},
		{"ssh connect timed out", nil, "ssh: connect to host github.com port 22: Connection timed out\nfatal: Could not read from remote repository.", ClassConnect, true},
		{"ssh banner exchange", nil, "kex_exchange_identification: Connection closed by remote host\nfatal: Could not read from remote repository.", ClassConnectionReset, true},

		// Real stderr of a connection cut mid-transfer, captured from git against
		// git http-backend (#8091 review F1): the transport line comes first and
		// git's pack follow-on lines come after it, so the class is the transport's.
		{"real: content-length cut in the pack header", nil, "error: RPC failed; curl 18 transfer closed with 820247 bytes remaining to read\nerror: 29 bytes of body are still expected\nfetch-pack: unexpected disconnect while reading sideband packet\nfatal: protocol error: bad pack header", ClassConnectionReset, true},
		{"real: content-length cut at 70 percent of the pack", nil, "remote: Enumerating objects: 4, done.\nremote: Counting objects: 100% (4/4), done.\nremote: Compressing objects: 100% (4/4), done.\nerror: RPC failed; curl 18 transfer closed with 246080 bytes remaining to read\nerror: 15892 bytes of body are still expected\nfetch-pack: unexpected disconnect while reading sideband packet\nfatal: early EOF\nfatal: unpack-objects failed", ClassConnectionReset, true},
		{"real: chunked cut at 70 percent of the pack", nil, "remote: Enumerating objects: 4, done.\nremote: Compressing objects: 100% (4/4), done.\nerror: RPC failed; curl 18 transfer closed with outstanding read data remaining\nerror: 15894 bytes of body are still expected\nfetch-pack: unexpected disconnect while reading sideband packet\nfatal: early EOF\nfatal: unpack-objects failed", ClassConnectionReset, true},
		{"recv failure then early EOF and index-pack failed", nil, "error: RPC failed; curl 56 Recv failure: Connection reset by peer\nfatal: the remote end hung up unexpectedly\nfatal: early EOF\nfatal: index-pack failed", ClassConnectionReset, true},
		{"curl 28 low-speed abort", nil, "error: RPC failed; curl 28 Operation too slow. Less than 1 bytes/sec transferred the last 60 seconds\nfatal: the remote end hung up unexpectedly", ClassLowSpeedAbort, true},
		{"curl 92 http2 stream not closed cleanly", nil, "error: RPC failed; curl 92 HTTP/2 stream 5 was not closed cleanly: CANCEL (err 8)\nfatal: expected flush after ref listing", ClassHTTP2Framing, true},

		// Not retryable.
		{"pack follow-on lines without a transport line", nil, "fatal: early EOF\nfatal: unpack-objects failed", ClassPackObject, false},
		{"bad pack header without a transport line", nil, "fatal: protocol error: bad pack header", ClassPackObject, false},
		{"a true integrity error beside a transport line is not retried", nil, "error: RPC failed; curl 56 Recv failure: Connection reset by peer\nfatal: bad object 0123456789abcdef0123456789abcdef01234567", ClassPackObject, false},
		{"early EOF alone is not named", nil, "fatal: early EOF", ClassOther, false},
		{"hung up alone", nil, "fatal: the remote end hung up unexpectedly", ClassOther, false},
		{"http 403", nil, "fatal: unable to access 'https://github.com/o/r.git/': The requested URL returned error: 403", ClassAuth, false},
		{"http 401", nil, "remote: Invalid username or password.\nfatal: Authentication failed for 'https://github.com/o/r.git/'", ClassAuth, false},
		{"missing credentials", nil, "fatal: could not read Username for 'https://github.com': terminal prompts disabled", ClassAuth, false},
		{"ssh permission denied", nil, "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.", ClassAuth, false},
		{"http 404", nil, "fatal: unable to access 'https://github.com/o/r.git/': The requested URL returned error: 404", ClassHTTPClient, false},
		{"http 400", nil, "fatal: unable to access 'https://github.com/o/r.git/': The requested URL returned error: 400", ClassHTTPClient, false},
		{"repository not found", nil, "remote: Repository not found.\nfatal: repository 'https://github.com/o/r.git/' not found", ClassNotFound, false},
		{"http 429", nil, "fatal: unable to access 'https://github.com/o/r.git/': The requested URL returned error: 429", ClassRateLimited, false},
		{"rate limit text", nil, "remote: API rate limit exceeded for installation.\nfatal: unable to access", ClassRateLimited, false},
		{"bad object", nil, "fatal: bad object 0123456789abcdef0123456789abcdef01234567", ClassPackObject, false},
		{"index-pack failed", nil, "fatal: fetch-pack: invalid index-pack output\nerror: index-pack failed", ClassPackObject, false},
		{"pack has unresolved delta", nil, "fatal: pack has 1 unresolved delta", ClassPackObject, false},
		{"http 503 is out of v1", nil, "fatal: unable to access 'https://github.com/o/r.git/': The requested URL returned error: 503", ClassOther, false},
		{"a 4xx beside a network line is not retried", nil, "Could not resolve host: other.example\nThe requested URL returned error: 403", ClassAuth, false},
		{"unknown text", nil, "fatal: something nobody has seen", ClassOther, false},
		{"plain process error", errors.New("exit status 1"), "", ClassOther, false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			class, retry := Classify(row.err, row.stderr)
			if class != row.wantClass || retry != row.wantRetry {
				t.Fatalf("Classify(%v, %q) = %q, %v; want %q, %v", row.err, row.stderr, class, retry, row.wantClass, row.wantRetry)
			}
		})
	}
}
