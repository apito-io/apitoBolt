#!/usr/bin/env bash
set -euo pipefail

command -v goreleaser >/dev/null 2>&1 || { echo >&2 "GoReleaser is required: https://goreleaser.com"; exit 1; }

go mod tidy
go test ./...

goreleaser release --clean


