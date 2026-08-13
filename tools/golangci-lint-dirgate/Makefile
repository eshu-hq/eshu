# Build the dirgate Go plugin consumed by golangci-lint via
# linters.settings.custom.dirgate.path in go/.golangci.yml.
#
# This mirrors ../golangci-lint-filelength/Makefile exactly -- see that
# file's comments for why a Makefile (not "go build") and why this is a
# separate module from go/Makefile. Keep both Makefiles in lockstep.
.PHONY: build test clean

GO        ?= go
PLUGIN    ?= dirgate.so
CGO_FLAG  ?= CGO_ENABLED=1

build:
	$(CGO_FLAG) $(GO) build -buildmode=plugin -o $(PLUGIN) .

test:
	$(GO) test ./...

clean:
	rm -f $(PLUGIN)
