#!/usr/bin/env bash
set -euo pipefail

gofmt -l src cmd tests | tee /tmp/anchordtl-gofmt.txt
test ! -s /tmp/anchordtl-gofmt.txt
go test ./...
go vet ./...
