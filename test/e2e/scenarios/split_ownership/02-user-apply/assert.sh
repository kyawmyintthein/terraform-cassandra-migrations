#!/usr/bin/env bash

set -euo pipefail

source "${ROOT_DIR}/test/e2e/assertions.sh"

assert_cql_contains \
  "SELECT keyspace_name FROM system_schema.keyspaces WHERE keyspace_name = 'app_split_e2e';" \
  "app_split_e2e" \
  "user-level keyspace should exist"

assert_cql_contains \
  "SELECT table_name FROM system_schema.tables WHERE keyspace_name = 'app_split_e2e' AND table_name = 'events';" \
  "events" \
  "user-level table should exist"

assert_cql_contains \
  "SELECT index_name FROM system_schema.indexes WHERE keyspace_name = 'app_split_e2e' AND table_name = 'events' AND index_name = 'events_event_type_sai';" \
  "events_event_type_sai" \
  "SAI index should exist"
