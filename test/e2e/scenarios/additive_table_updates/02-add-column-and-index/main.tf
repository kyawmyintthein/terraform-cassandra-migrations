terraform {
  required_providers {
    cassandra = {
      source = "kyawmyintthein/cassandra"
    }
  }
}

provider "cassandra" {
  hosts                    = ["cassandra"]
  port                     = 9042
  local_datacenter         = "datacenter1"
  system_metadata_keyspace = "tf_meta_update"
  system_metadata_replication = {
    class              = "SimpleStrategy"
    replication_factor = "1"
  }
  migration_lock_keyspace = "tf_lock_update"
  migration_lock_table    = "schema_migration_locks"
}

resource "cassandra_system_level_migration_lock_store" "schema" {
  keyspace   = "tf_lock_update"
  table_name = "schema_migration_locks"

  replication = {
    class              = "SimpleStrategy"
    replication_factor = "1"
  }
}

resource "cassandra_system_level_keyspace_policy" "regional" {
  name = "regional"

  replication_class  = "SimpleStrategy"
  replication_factor = 1
}

resource "cassandra_system_level_profile" "write_heavy" {
  name = "write_heavy"

  compaction = {
    class = "org.apache.cassandra.db.compaction.SizeTieredCompactionStrategy"
  }
}

resource "cassandra_user_level_keyspace" "app" {
  depends_on = [
    cassandra_system_level_keyspace_policy.regional,
    cassandra_system_level_migration_lock_store.schema,
  ]

  keyspace                        = "app_update_e2e"
  if_not_exists                   = true
  required_system_keyspace_policy = "regional"
}

resource "cassandra_user_level_table" "events" {
  depends_on = [
    cassandra_user_level_keyspace.app,
    cassandra_system_level_profile.write_heavy,
  ]

  keyspace                = "app_update_e2e"
  table_name              = "events"
  if_not_exists           = true
  required_system_profile = "write_heavy"

  columns = [
    {
      name = "tenant_id"
      type = "text"
    },
    {
      name = "event_id"
      type = "timeuuid"
    },
    {
      name = "payload"
      type = "text"
    },
    {
      name = "event_type"
      type = "text"
    }
  ]

  partition_keys = ["tenant_id"]

  clustering_keys = [
    {
      name  = "event_id"
      order = "DESC"
    }
  ]

  sai_indexes = [
    {
      name   = "events_event_type_sai"
      column = "event_type"
    }
  ]
}
