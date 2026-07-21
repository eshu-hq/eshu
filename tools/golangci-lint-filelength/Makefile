# Build the filelength Go plugin consumed by golangci-lint via
# linters.settings.custom.filelength.path in go/.golangci.yml.
#
# Why a Makefile and not "go build":
#   - `-buildmode=plugin` requires CGO_ENABLED=1, which is not the
#     default for many developer shells.
#   - The plugin must be built with the same `golang.org/x/tools`
#     revision that the pinned golangci-lint release vendors,
#     otherwise plugin.Open fails with "plugin was built with a
#     different version of package ...". That revision is pinned in
#     this module's go.mod, whose comment names the golangci-lint
#     version it matches; CI installs that same version in
#     .github/workflows/test.yml. Those two must be bumped together.
#     No version is restated here on purpose: a number duplicated
#     into this comment goes stale on the next bump and then points
#     whoever is debugging a plugin.Open failure at the wrong
#     release. This Makefile just makes the build deterministic.
#
# Why this is not in go/Makefile:
#   - tools/golangci-lint-filelength is a separate Go module so its
#     dependency graph (golang.org/x/tools) does not pollute the
#     main module's go.sum.
#   - The plugin binary (.so) is a build artifact and is not
#     versioned; only the .go sources are.
.PHONY: build test clean

GO        ?= go
PLUGIN    ?= filelength.so
CGO_FLAG  ?= CGO_ENABLED=1

build:
	$(CGO_FLAG) $(GO) build -buildmode=plugin -o $(PLUGIN) .

test:
	$(GO) test ./...

clean:
	rm -f $(PLUGIN)
