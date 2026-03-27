#!/usr/bin/env bash

set -euo pipefail

source "${ROOT_DIR}/test/e2e/assertions.sh"

assert_cql_contains \
  "SELECT column_name FROM system_schema.columns WHERE keyspace_name = 'app_update_e2e' AND table_name = 'events' AND column_name = 'event_type';" \
  "event_type" \
  "updated table should include the additive event_type column"

assert_cql_contains \
  "SELECT index_name FROM system_schema.indexes WHERE keyspace_name = 'app_update_e2e' AND table_name = 'events' AND index_name = 'events_event_type_sai';" \
  "events_event_type_sai" \
  "updated table should include the new SAI index"
