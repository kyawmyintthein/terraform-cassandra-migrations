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

resource "cassandra_system_level_table_settings" "events_storage" {
  keyspace   = "app_split_e2e"
  table_name = "events"
  comment    = "Managed by admin settings"

  additional_options = {
    compression = "{'class':'LZ4Compressor','chunk_length_in_kb':'64'}"
  }
}
