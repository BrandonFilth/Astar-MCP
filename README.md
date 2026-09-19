# Astar MCP

Read-only tooling for Astar, with optional Soneium ecosystem coverage.

The MCP service is not implemented yet. This repository currently contains source
verification scripts and a Go client that probes the official GitHub MCP server.

## Requirements

- Go 1.25.12 (the Go toolchain can download the pinned version automatically).
- Python 3 for verification and installation scripts.
- Linux amd64 for the supplied GitHub MCP binary installer.

## Local checks

Run `make check` to analyze and compile the Go packages. The current probe package
does not yet contain unit tests; this command is not an integration test.

Run `make install-github` to download the pinned official GitHub MCP binary and
verify its archive checksum. Generated binaries are excluded from version control.

## Source verification

`make probe-sources` checks the ecosystem directory, Aradia API, and Astar network.
It uses `ARADIA_API_TOKEN` when configured; otherwise it requests one temporary
demo token. Demo credentials remain in memory. These checks make live requests
and are subject to provider availability and rate limits.

`make probe-github` connects to the official GitHub MCP server. Configure:

- `GITHUB_PERSONAL_ACCESS_TOKEN`: a dedicated GitHub read credential.
- `GITHUB_MCP_BINARY`: the absolute path to the installed binary.
- `GITHUB_PROBE_PR`: an existing pull request number in `AstarNetwork/Astar`.

The probe checks tool discovery, file access, pull requests, changed files, and
reviews. Missing credentials produce an explicit failure.

Results are written to `.local/evidence/`, which is excluded from version control.
`.env.example` lists configuration variables; environment files are not loaded
automatically. Never commit credentials or include them in reports.
