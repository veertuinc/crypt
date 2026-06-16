# Contributing to Crypt

Thank you for your interest in Crypt. This guide covers how to build, test, and
submit changes.

## License

Crypt is **source-available** under the
[PolyForm Noncommercial License 1.0.0](https://polyformproject.org/licenses/noncommercial/1.0.0).
See [LICENSE](LICENSE) for the full terms.

By contributing code, documentation, or other material to this repository, you
agree that your contribution is licensed under the same terms. Commercial use of
Crypt requires a separate license from Veertu Inc.

## Prerequisites

- **Go 1.25+** (see the `go` directive in [`go.mod`](go.mod)).
- **make** (optional but recommended — see the [Makefile](Makefile)).
- macOS for end-to-end runs. The unit tests are platform-independent and run
  anywhere Go does; only an actual `crypt` invocation needs macOS + Anka.
- **Anka Virtualization 3.9+** and a prepared base VM if you want to exercise a
  real run (see [README.md](README.md) for VM setup).

## Project layout

```text
main.go                 # entrypoint: wires signal handling into cli.Execute
Makefile                # build, install, test, and lint shortcuts
internal/
  cli/                  # cobra command tree (agent subcommands + `run`)
  agent/                # registry of supported agents and their inject flags
  sandbox/              # provision → mount → run → guaranteed teardown
  anka/                 # thin binding over the `anka` CLI
  tty/                  # PTY bridge for full-screen agent TUIs
  meta/                 # build-time version/commit/date metadata
packaging/
  goreleaser.yaml       # release + Homebrew cask configuration
```

## Quick start

Run `make help` to list all targets. The common workflow:

```sh
make build      # build ./crypt with version metadata
make test       # run unit tests
make install    # install crypt onto $GOPATH/bin or $GOBIN
```

## Building

The Makefile stamps version info via `-ldflags` into `internal/meta` (matching
release builds):

```sh
make build
./crypt --version
```

Install onto your `PATH`:

```sh
make install
```

Remove build artifacts:

```sh
make clean
```

### Without Make

```sh
go build -o crypt .
go install .
go run . --help
```

To reproduce the version metadata by hand:

```sh
go build -ldflags "\
  -X github.com/veertuinc/crypt/internal/meta.version=dev \
  -X github.com/veertuinc/crypt/internal/meta.commit=$(git rev-parse --short HEAD) \
  -X github.com/veertuinc/crypt/internal/meta.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o crypt .
```

Release builds stamp the git tag (e.g. `v1.2.3`); dev builds use `dev`.
Without ldflags, `crypt --version` falls back to embedded build info (or `dev`),
so the command always works.

## Testing

```sh
make test           # run all unit tests
make test-v         # verbose output
make test-race      # race detector, no caching
make test-cover     # coverage summary per package
make cover          # write cover.out and cover.html
```

Run a single package:

```sh
go test ./internal/anka/...
```

### What the tests cover

The unit tests target the deterministic, logic-heavy code and avoid needing a
real VM:

- `internal/agent` — agent lookup, sorted listing, and command composition.
- `internal/anka` — version parsing, version comparison, VM existence checks,
  error surfacing, and `anka run` argument construction. These tests drive a
  **fake `anka` shell script** written to a temp dir, so no Anka install is
  required.
- `internal/sandbox` — clone-name sanitization, ephemeral name generation, and
  login-shell quoting/escaping.
- `internal/meta` — version resolution: git tag on release builds, `dev` otherwise, with ldflag overrides.

`internal/tty` (real PTY/terminal I/O) and the full `sandbox.Run` orchestration
are not unit-tested because they require a live terminal and Anka host.

## Linting & formatting

```sh
make vet
make fmt
```

If you have [`golangci-lint`](https://golangci-lint.run) installed:

```sh
golangci-lint run
```

## Adding a new agent

Agents are data, not code paths. Add a single entry to the `registry` map in
[`internal/agent/registry.go`](internal/agent/registry.go) with the executable
`Name`, a `Summary`, and the `InjectFlags` that put it into unattended mode. The
CLI builds a subcommand for it automatically — no new command file needed. Add a
test case alongside the existing ones in `registry_test.go`.

## Submitting changes

1. Fork the repository and create a branch for your change.
2. Run `make test` (and `make test-race` when touching concurrent code).
3. Run `make vet` and `make fmt`.
4. Open a pull request with a clear description of what changed and why.

## Releasing

Releases are produced with [GoReleaser](https://goreleaser.com) using
[`packaging/goreleaser.yaml`](packaging/goreleaser.yaml), which builds macOS
`arm64` and `amd64` archives and publishes the Homebrew cask.

Dry-run a snapshot build without publishing:

```sh
goreleaser release --snapshot --clean --config packaging/goreleaser.yaml
```

Cut a real release by tagging and pushing:

```sh
git tag v1.2.3
git push origin v1.2.3
goreleaser release --clean --config packaging/goreleaser.yaml
```
