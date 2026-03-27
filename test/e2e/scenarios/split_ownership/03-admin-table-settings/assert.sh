#!/usr/bin/env bash

set -euo pipefail

source "${ROOT_DIR}/test/e2e/assertions.sh"

assert_cql_contains \
  "SELECT comment FROM system_schema.tables WHERE keyspace_name = 'app_split_e2e' AND table_name = 'events';" \
  "Managed by admin settings" \
  "system-level table settings should update the table comment"
