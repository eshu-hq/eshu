# #8986: one per-file read cap in the shared parser read path

## Problem

`shared.ReadSource` called `os.ReadFile` with no size limit, and the git
collector read each parse body and each content-stream body the same way.
Discovery recorded each file's size but used it only to weight parse
partitions. A parser without its own byte cap, or a large file under an
admitted name, cost memory proportional to its size in every parse worker that
touched it.

## Fix

- `go/internal/parser/shared/read_cap.go` adds `MaxSourceBytes` (64 MiB),
  `ErrSourceTooLarge`, `SourceReadCap`, and `ReadFileCapped`. `ReadSource`
  refuses a file over the cap on both the disk path and the primed path.
- The git collector removes a parser file over the cap after
  `partitionNativeSnapshotFiles`, before the import pre-scan, in full and delta
  mode. Terraform state candidates (metadata-only) and documentation files
  (bounded readers) are not affected.
- The parse read and the fact-stream content read use `ReadFileCapped`, so a
  file that grew past the cap after discovery is refused, not read whole.
- A delta target over the cap joins the delta's deleted paths, so the stream
  emits its file and content tombstones and the graph retracts it. A full
  generation already drops an absent file; both end with the file absent.
- Parser side reads of other repository files use `ReadFileCapped`: the C and
  C++ header scans, `go.mod`, the JavaScript sibling parser, tsconfig and
  package.json, composer.json and composer.lock, the Python package `__init__`
  and Lambda config scans. `TestParserTreeReadsRepositoryFilesThroughTheCap`
  fails on a new raw whole-file read in the parser tree, under any import name
  (default, alias, or dot import).
- A delta's workflow image metadata, built from the full discovery set for
  workflows the delta does not target, leaves out a workflow over the cap
  without a read failure, as a full snapshot does, and its digest read is
  capped. Before, the delta hashed the whole file, the capped stream re-read
  refused it, and every delta counted `workflow_read_failed` and dropped the
  pre-parse identity.

A first cut that capped only `ReadSource` failed the whole snapshot: the import
pre-scan reads every parser file and returns an error on the first failure
(`pre-scan repository imports ...: source file exceeds the per-file read cap`).
That is why the skip runs at the parser file set, before pre-scan.

## Cap measurement

- Largest lock file in the golden corpus trees (`examples/`, `tests/fixtures`):
  743 bytes (`examples/supply-chain-demo/missing-evidence/package-lock.json`).
- Largest lock file in the repository: 274,035 bytes (root
  `package-lock.json`); the parser large-lockfile fixture is 276,647 bytes.
- Largest lock file across the other local checkouts on the build host (50 lock
  files): the same 274,035 bytes.

64 MiB leaves more than two hundred times headroom over every measured lock
file. No environment variable was added: no operator need for one was shown.

## Read-path cost

Performance Evidence: baseline is `os.ReadFile` on `origin/main`; after is
`shared.ReadFileCapped`. Backend: none; no Cypher, graph write, Postgres SQL,
queue, lease, or worker count changes, so there are no terminal queue or row
counts to compare. Input shape: one regular file of 1 KiB, 64 KiB, or 4 MiB.
The content-version fixture stream stays at 320 facts with digest
`755a5940...`, so collector output for files under the cap is unchanged.

A throwaway benchmark (`-benchtime 2000x -count 3`, 16-thread Linux host)
compared `os.ReadFile` with the capped read (stat, buffer sized from the stat,
`io.LimitReader` at cap+1):

| File | `os.ReadFile` | capped read |
| --- | --- | --- |
| 1 KiB | 3,521-3,707 ns, 1,512 B, 5 allocs | 3,544-3,611 ns, 1,536 B, 6 allocs |
| 64 KiB | 9,047-11,853 ns, 74,088 B, 5 allocs | 9,318-9,414 ns, 74,112 B, 6 allocs |
| 4 MiB | 467,088-594,208 ns, 4,202,865 B, 5 allocs | 476,407-541,418 ns, 4,202,887 B, 6 allocs |

The capped read adds 24 bytes and one allocation per file (the limited reader)
and no measurable time. A repository-scale collector wall-time comparison was
not run.

## Observability

Observability Evidence: each skipped file adds one to `DiscoveryStats.FilesSkippedByContent["oversize"]`,
which feeds `eshu_dp_discovery_files_skipped_total{skip_reason="content:oversize"}`
and the `discovery stats` log key `files_skipped.content.oversize`. After the
generation exists, `snapshotOneRepository` logs WARN
`git_snapshot_file_skipped_oversize` per file with `repository_id`, `scope_id`,
`generation_id`, `relative_path`, `size_bytes`, and `cap_bytes`, never content
or the host path. A size skip is deterministic for the commit, so it keeps the
pre-parse identity.

## Known limits

- A file that grows past the cap between discovery and the import pre-scan
  fails that snapshot, as any unreadable parser file does. The failure is
  transient: the next cycle's discovery records the new size and skips the file.
- A `.gitmodules` or CODEOWNERS file over the cap in the full discovery set is
  counted as an oversize skip although those files are read by their own
  readers; files of that size are not realistic.

## Reads left outside the cap

These read whole files but are not parser input:

- `parser/hcl/include_chain.go`: Terragrunt includes, behind their own
  Lstat-checked 1 MiB cap.
- `parser/scip/parser.go`: the SCIP index the indexer wrote, not repository
  content.
- `parser/goldenaudit/golden_audit.go`: checked-in golden fixtures.
- `collector/repo/git/snapshot_native_helpers.go` (`PreparseConfigFor`): the
  digest of an operator-installed SCIP binary.
- `collector/repo/git/snapshot_native_vendor_roots.go`, `selection_ignore.go`,
  `selection_copy_commit.go`, `discovery/gitignore.go`: `.eshu/discovery.json`,
  `.gitignore`, and `.eshuignore` control files.
- `collector/repo/git/selection_filesystem.go`: the collector's own fixture
  manifest.
