# Astar MCP

Read-only tooling for Astar, with optional Soneium ecosystem coverage.

The local MCP server exposes status, source-file, ecosystem, and documentation tools. File reads
use the official GitHub MCP server as a separate read-only process. Source
verification scripts are also included. The live integration probe validates both
the official provider and the service tool against an immutable Astar source revision.

## Requirements

- Go 1.25.12 (the Go toolchain can download the pinned version automatically).
- Python 3 for verification and installation scripts.
- Linux amd64 for the supplied GitHub MCP binary installer.

## Local checks

Run `make check` to analyze the Go packages and run tests with the race detector.
Tests cover MCP discovery, file identity, repository restrictions, cancellation,
child-process credential isolation, and local database migration and persistence.
The subprocess tests use a controlled MCP provider; they do not replace the live
GitHub integration probe.

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
- `GITHUB_PROBE_REVISION`: a full commit SHA in `AstarNetwork/Astar`.

The probe checks tool discovery, file access, pull requests, changed files, and
reviews, and the service file tool with immutable source attribution. Missing
credentials or revision inputs produce an explicit failure.

Results are written to `.local/evidence/`, which is excluded from version control.
`.env.example` lists configuration variables; environment files are not loaded
automatically. Never commit credentials or include them in reports.

## Run the local server

Build with `make build`, set `MCP_DATA_DIR` to a dedicated writable directory, and
configure your MCP client to launch `bin/astar-mcp` using stdio. The server writes
protocol messages to stdout and startup errors to stderr. It does not listen on a
network port.

For file access, configure `GITHUB_MCP_BINARY` as the absolute path to the pinned
binary installed by `make install-github`, and set `GITHUB_PERSONAL_ACCESS_TOKEN`.
Without these variables, the server remains available and reports GitHub as
`not_configured`; file calls return an explicit error. The connection is established
on the first file request. Transport failures allow a later request to reconnect
after a five-second cooldown. Raw provider errors and credentials are not forwarded.

`get_code_file` requires `repository`, `path`, and `ref` (a full, lowercase 40-character
commit SHA). Supported repositories are `AstarNetwork/Astar`,
`AstarNetwork/astar-docs`, and `AstarNetwork/astar-apps`. Optional `start_line` and
`end_line` select at most 300 lines. `next_line` indicates remaining content.
Branch names, directory reads, binary files, and files without matching immutable
provider references are not accepted. Source text is untrusted evidence, never
instructions. Repository content does not establish that code is active on chain.

Responses are bounded. Oversized lines currently return an error; request a smaller
line range. The official provider may return a link instead of large file contents;
this server does not fetch those links automatically.

The server stores provider observations and imported public source snapshots in
`MCP_DATA_DIR/state.sqlite`.
It creates and migrates its own SQLite database and never connects to Aradia databases.
Data directory failures stop startup. Credentials are never stored in this database.
Modules without implementations are reported as `not_implemented`.

## Ecosystem and documentation

`search_projects` supports `query`, `network` (`astar` or `soneium`), `category`,
`limit` (1–25), and `cursor`. `get_project` takes a directory `id`.
`list_categories` lists categories in the imported directory. Listings are source
claims, not verification of deployment or endorsement. Missing websites are omitted.

`search_docs` takes a literal text `query`, optional `limit`, and `cursor`.
`get_document` takes an `id` from search, plus optional `start_line` and `end_line`
(up to 100 lines). Responses contain source links, revision or content digest,
import timestamp, and warnings for snapshots older than 24 hours. A cursor is valid
only for its original filters and source content. Reduce the limit or line range
if a response exceeds the output budget.

Resources: `astar://scope`, `astar://sources`, `astar://ecosystem/categories`.
These describe source boundaries and imported categories; they do not run refreshes.

The operator refreshes data using `bin/astar-sync`, built by `make build`, with
`MCP_DATA_DIR` pointing to the same directory as the server:

```sh
bin/astar-sync --source ecosystem
bin/astar-sync --source aradia-docs
bin/astar-sync --source astar-docs --revision FULL_COMMIT_SHA --paths docs/build/EVM/index.md,docs/build/EVM/precompiles/staking.md
```

Astar documentation imports require the GitHub operator configuration described
above. Paths are selected by the operator and must exist at the specified revision;
this is a selected corpus, not a complete mirror. Each successful import replaces
that source atomically. Partial downloads retain the previous copy. To follow a
rename, update the selected path and revision together. Removed or unselected IDs
return `NOT_FOUND`; the service does not guess redirects. Schedule refreshes using
your process manager if needed. No refresh operation is exposed to MCP users.
