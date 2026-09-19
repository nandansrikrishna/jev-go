# Install Jev Go v0.1.0-beta.1

Download an archive and `SHA256SUMS` from:
https://github.com/nandansrikrishna/jev-go/releases/tag/v0.1.0-beta.1

| Your computer | Archive suffix |
| --- | --- |
| macOS, Apple Silicon | darwin_arm64.tar.gz |
| macOS, Intel | darwin_amd64.tar.gz |
| Linux, Intel/AMD 64-bit | linux_amd64.tar.gz |
| Linux, ARM 64-bit | linux_arm64.tar.gz |
| Windows, Intel/AMD 64-bit | windows_amd64.zip |
| Windows, ARM 64-bit | windows_arm64.zip |

Each archive includes `jev` (Windows: `jev.exe`), `LICENSE`, `INSTALL.md`,
`README.md`, and `THIRD_PARTY_NOTICES.md`. Go and Python are not required to run the binary.
A TypeSafe API key and an internet connection are required for evaluation.

## macOS and Linux

Verify the archive against the matching entry in `SHA256SUMS`:

```sh
# macOS example:
shasum -a 256 jev_0.1.0-beta.1_darwin_arm64.tar.gz
# Linux example:
sha256sum jev_0.1.0-beta.1_linux_amd64.tar.gz
```

Extract the archive for your machine, then install the binary in a directory
on your PATH. For example on Apple Silicon:

```sh
tar -xzf jev_0.1.0-beta.1_darwin_arm64.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 755 jev "$HOME/.local/bin/jev"
export PATH="$HOME/.local/bin:$PATH"
jev --version
```

Add the PATH setting to your shell configuration to keep it across terminals.
The macOS binaries are not Apple-signed or notarized. If macOS blocks one,
verify the release and checksum, then use the approval option in System Settings
under Privacy & Security according to your organization's policy.

## Windows (PowerShell)

Compute the hash and compare it to the matching entry in `SHA256SUMS`:

```powershell
Get-FileHash .\jev_0.1.0-beta.1_windows_amd64.zip -Algorithm SHA256
Expand-Archive .\jev_0.1.0-beta.1_windows_amd64.zip -DestinationPath .\jev
.\jev\jev.exe --version
```

Use the ARM64 archive for Windows on ARM. Move `jev.exe` to a permanent directory
and add that directory to your user PATH if you want to run it as `jev`.
The Windows binary is not Authenticode-signed.

## Authenticate and try it

```sh
jev auth
jev init
jev validate --questions jev-demo/questions.json --input jev-demo/tickets.jsonl
jev evaluate --questions jev-demo/questions.json --input jev-demo/tickets.jsonl --output results.jsonl
```

`jev auth` reads your key without echoing it. macOS/Linux store it in
`~/.config/jev/api-key` with owner-only permissions; Windows uses Credential
Manager. `TYPESAFE_API_KEY` overrides saved credentials. For automation,
`jev auth --stdin` reads a key from standard input. Do not put keys in source
files or MCP arguments. Evaluation uses your TypeSafe account's API quota.

The generated `summarize.py` is optional and requires Python; it is not used by
the CLI or MCP server. Results are JSONL and can be processed by any tool.

## MCP setup

The same binary provides a native stdio MCP server:

```sh
codex mcp add jev -- jev mcp
```

Other clients can register it with an absolute binary path:

```json
{"mcpServers":{"jev":{"command":"/absolute/path/to/jev","args":["mcp"]}}}
```

On Windows, use an escaped path such as `C:\\Tools\\jev\\jev.exe` in JSON.
Tools: `question_schema`, `evaluate`, and `evaluate_batch` (1–50 records).
No Python package or sibling repository is needed.

## Build from source

Go 1.25 or newer is required only when building:

```sh
go install github.com/nandansrikrishna/jev-go/cmd/jev@v0.1.0-beta.1
```

The executable is installed in GOBIN, or `$(go env GOPATH)/bin` by default.
Add that directory to PATH. Both the Python integration and Go CLI use the name
`jev`; check which binary your PATH selects if you have both installed.
