# #6052 — the chart's default image tag stopped tracking `appVersion`

## What was wrong

`deploy/helm/eshu/values.yaml` pinned `image.tag: "v0.0.2"` from the
`chore(release): prepare v0.0.2` commit onward and was never touched by any
later release-prep commit, while `deploy/helm/eshu/Chart.yaml`'s `appVersion`
kept advancing through the `v0.0.3-pre-release-N` series. A plain
`helm install`/`helm template` with no `--set image.tag=...` override
therefore rendered pods running `ghcr.io/eshu-hq/eshu:v0.0.2` labeled
`app.kubernetes.io/version: v0.0.3-pre-release-18` — the label the chart
itself writes (`deploy/helm/eshu/templates/_helpers.tpl`, from
`.Chart.AppVersion`) did not describe the image the same chart pulled by
default. Confirmed against git history
(`git log -p origin/main -- deploy/helm/eshu/values.yaml`): the `image.tag`
line changed exactly once, at the v0.0.2 release commit, across 18+ later
appVersion bumps.

## What changed

`values.yaml`'s default `image.tag` now matches the `appVersion` this PR
bumps to (`v0.0.3-pre-release-18`), and
`docs/public/deploy/kubernetes/helm-values.md`'s documented default is
updated to match, so the rendered image and the `app.kubernetes.io/version`
label agree again on a plain install.

## No-Regression Evidence

No-Regression Evidence: this is a string-literal default-value change in a
values file, not a code or template-logic change — no algorithm, query, or
control flow moved. Verified by rendering the chart before and after on this
branch:

```bash
helm lint deploy/helm/eshu
helm template eshu deploy/helm/eshu | rg 'image: "ghcr.io/eshu-hq/eshu'
helm template eshu deploy/helm/eshu | rg 'app.kubernetes.io/version'
```

Before: `image: "ghcr.io/eshu-hq/eshu:v0.0.2"` next to
`app.kubernetes.io/version: "v0.0.3-pre-release-18"` (mismatch reproduced).
After: both render `v0.0.3-pre-release-18` (match). `helm lint` passes with
0 failed charts on both.

## Observability Evidence

No-Observability-Change: a values default has no telemetry surface of its
own; the label this fix corrects is metadata Kubernetes/Helm attach to the
rendered manifest, not something the running binaries emit.
