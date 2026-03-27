#!/usr/bin/env bash

set -euo pipefail

source "${ROOT_DIR}/test/e2e/assertions.sh"

assert_cql_contains \
  "SELECT column_name FROM system_schema.columns WHERE keyspace_name = 'app_update_e2e' AND table_name = 'events' AND column_name = 'payload';" \
  "payload" \
  "base table should include payload column"
