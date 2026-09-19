# Go CLI releases

1. Update `Version` in `internal/cli/cli.go`, installation examples, and changelog.
   Use SemVer, including prerelease suffixes (for example, `0.1.0-beta.1`).
2. Run `go test -race -timeout 120s ./...`, `go vet ./...`, and optional Python
   interoperability tests. No credentials are needed for automated tests.
3. Build archives using `go run ./tools/release`. Go 1.25+ is required.
   This cross-compiles six targets into `dist/`, with license and installation
   documentation in each archive, plus a `SHA256SUMS` file.
4. Commit and push. Wait for all three OS jobs in Go tests to pass.
5. Tag the tested commit `v<VERSION>` and push the tag.
6. Create a draft GitHub release, upload all archives and `SHA256SUMS`, and verify
   downloaded assets match their hashes. Publish as a prerelease for beta versions.

The repository's CI tests run on macOS, Linux, and Windows. ARM64 Linux/Windows
and Intel macOS artifacts are cross-compiled; do not claim native execution
coverage for architectures not exercised by CI. Public binaries are currently
unsigned (no Apple notarization or Windows Authenticode signing).

All commands, including `jev mcp`, run without Python. The Python integration
has its own repository and release process.
