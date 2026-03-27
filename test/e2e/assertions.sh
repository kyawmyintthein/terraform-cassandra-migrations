#!/usr/bin/env bash

set -euo pipefail

if [[ -z "${ROOT_DIR:-}" ]]; then
  echo "ROOT_DIR must be set before sourcing assertions.sh" >&2
  exit 1
fi

if [[ -z "${COMPOSE_FILE:-}" ]]; then
  echo "COMPOSE_FILE must be set before sourcing assertions.sh" >&2
  exit 1
fi

cql_query() {
  local query="$1"

  docker compose -f "${COMPOSE_FILE}" exec -T cassandra \
    cqlsh -e "${query}"
}

assert_cql_contains() {
  local query="$1"
  local expected="$2"
  local description="$3"
  local output

  output="$(cql_query "${query}")"
  if [[ "${output}" != *"${expected}"* ]]; then
    echo "Assertion failed: ${description}" >&2
    echo "Expected to find: ${expected}" >&2
    echo "Query: ${query}" >&2
    echo "Output:" >&2
    echo "${output}" >&2
    exit 1
  fi
}
