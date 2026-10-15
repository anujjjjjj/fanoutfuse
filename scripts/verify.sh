#!/bin/sh
set -eu
export CGO_ENABLED=0
export GOPATH="${GOPATH:-/tmp/fanoutfuse-go}"
export GOCACHE="${GOCACHE:-/tmp/fanoutfuse-cache}"
go build ./...
go vet ./...
go test ./...
go build -o /tmp/fanoutfuse-verify ./cmd/fanoutfuse
sandbox=$(mktemp -d)
trap 'rm -rf "$sandbox" /tmp/fanoutfuse-verify' EXIT
export FANOUTFUSE_HOME="$sandbox/state"
for fixture in fixtures/*.yaml; do
  /tmp/fanoutfuse-verify replay "$fixture"
done
/tmp/fanoutfuse-verify pm --md
printf '{"session_id":"69332","tool_name":"Agent"}\n' | /tmp/fanoutfuse-verify guard
