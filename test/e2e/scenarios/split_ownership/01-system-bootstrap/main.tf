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

resource "cassandra_system_level_migration_lock_store" "schema" {
  keyspace   = "tf_lock_split"
  table_name = "schema_migration_locks"

  replication = {
    class              = "SimpleStrategy"
    replication_factor = "1"
  }
}

resource "cassandra_system_level_keyspace_policy" "regional" {
  name = "regional"

  replication_class = "NetworkTopologyStrategy"
  durable_writes    = true

  region_replication_factors = {
    datacenter1 = "1"
  }
}

resource "cassandra_system_level_profile" "write_heavy" {
  name    = "write_heavy"
  comment = "Write-heavy baseline"

  compaction = {
    class = "org.apache.cassandra.db.compaction.SizeTieredCompactionStrategy"
  }

  gc_grace_seconds = 86400
}
