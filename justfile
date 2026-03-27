set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

default:
    @just --list

fmt:
    gofmt -w .

vet:
    go vet ./...

test:
    go test ./...

e2e *scenarios:
    ./scripts/e2e-test.sh {{scenarios}}
