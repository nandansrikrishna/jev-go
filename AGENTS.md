# Working with Jev Go

This repository contains the standalone Go CLI and MCP server. A local sibling
`../Jev 3` may contain the Python implementation; keep its working tree unchanged.

Build: `go build -o bin/jev ./cmd/jev`.
Tests: `go test -race ./...` and `go vet ./...`.
Optional Python compatibility tests: `../Jev 3/.venv/bin/python -m pytest -q tests`
(quote the interpreter path in the shell). Build the Go binary first.

`jev mcp` runs the native stdio server using the official MCP Go SDK.
All commands work without Python. Build tooling requires Go 1.25 or newer.
Credentials use TYPESAFE_API_KEY or the shared local Jev credential store.
Never print or commit keys, or read the credential file into agent context.

For evaluation, write typed questions and JSONL records with unique string IDs
and state, validate first, then evaluate. Aggregate in code and inspect source
records. Bundle independent questions per record; use --resume for retries.
