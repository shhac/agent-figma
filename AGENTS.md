# agent-figma

This repository contains a Go CLI that lets AI agents read Figma files: node trees and metadata, rendered node images, and image assets.

## Development

- Use `make build` to build `agent-figma`.
- Use `make test` for the full test suite.
- Use `make lint` (golangci-lint v2) and `make vet` before committing.
- Built on the shared family libraries: `lib-agent-cli` (root command, config, credentials), `lib-agent-output` (NDJSON/error wire contract).
- Do not print, log, or persist raw Figma tokens outside the credential backend.
- Keep list outputs compact and NDJSON-friendly; errors are `{error, fixable_by, hint?}` JSON on stderr.
