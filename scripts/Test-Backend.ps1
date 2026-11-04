$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path -Parent $PSScriptRoot
docker run --rm --mount "type=bind,source=$taskRoot\backend,target=/src" --mount 'type=volume,source=camerahub-gomod,target=/go/pkg/mod' --mount 'type=volume,source=camerahub-gocache,target=/root/.cache/go-build' --workdir /src golang:1.25-alpine sh -c 'go test -v ./... && go vet ./...'
if ($LASTEXITCODE -ne 0) { throw 'Backend checks failed.' }
