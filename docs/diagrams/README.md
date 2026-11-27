# docs/diagrams

Standalone diagram sources live here only when a diagram has no owning
page. The rule is **one picture, one home**:

- A flow chart lives inline in its Flow Atlas page
  (`docs/public/concepts/flows/`) or its package doc (e.g.
  `go/internal/reducer/queue-and-runners.md`) — never in both.
- A standalone `.mmd` file here must name its watcher and drift handling
  in its header comment (which script or gate fails when it goes stale,
  and what it is checked against). A file with no watcher gets deleted,
  not mirrored.
- When a diagram's system is removed, delete the file. Git history keeps
  it; a `HISTORICAL` banner is only for files under active discussion.
