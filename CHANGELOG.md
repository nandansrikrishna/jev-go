# Changelog

## v0.1.0-beta.1

First standalone Go release.

- Standalone Go CLI preserving the Python CLI commands, ordered JSONL results,
  credential storage, and compatible resume fingerprints.
- Native stdio MCP server using the official Go SDK v1.8.0; no Python runtime
  dependency. Exposes question_schema, evaluate, and evaluate_batch.
- Shared CLI/MCP validation, credentials, retries, cancellation, and ordered batch results.
- Build requirement updated to Go 1.25.
- Go regression tests and Python interoperability tests.
