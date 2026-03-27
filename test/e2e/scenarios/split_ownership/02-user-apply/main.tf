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
  system_metadata_keyspace = "tf_meta_split"
  system_metadata_replication = {
    class              = "SimpleStrategy"
    replication_factor = "1"
  }
  migration_lock_keyspace = "tf_lock_split"
  migration_lock_table    = "schema_migration_locks"
}

resource "cassandra_user_level_keyspace" "app" {
  keyspace                         = "app_split_e2e"
  if_not_exists                    = true
  required_system_keyspace_policy  = "regional"
  regions                          = ["datacenter1"]
}

resource "cassandra_user_level_table" "events" {
  depends_on = [cassandra_user_level_keyspace.app]

  keyspace                = "app_split_e2e"
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
      name = "event_type"
      type = "text"
    },
    {
      name = "payload"
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
