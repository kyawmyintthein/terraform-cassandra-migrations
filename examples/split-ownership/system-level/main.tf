terraform {
  required_providers {
    cassandra = {
      source = "kyawmyintthein/cassandra"
    }
  }
}

provider "cassandra" {
  hosts                    = ["127.0.0.1"]
  port                     = 9042
  local_datacenter         = "dc1"
  system_metadata_keyspace = "terraform_schema_migration_admin"
  system_metadata_replication = {
    class = "NetworkTopologyStrategy"
    dc1   = "3"
  }
  migration_lock_keyspace  = "terraform_schema_migration"
  migration_lock_table     = "schema_migration_locks"
}

resource "cassandra_system_level_migration_lock_store" "schema" {
  keyspace   = "terraform_schema_migration"
  table_name = "schema_migration_locks"

  replication = {
    class = "NetworkTopologyStrategy"
    dc1   = "3"
  }
}

resource "cassandra_system_level_keyspace_policy" "regional" {
  name = "regional"

  replication_class = "NetworkTopologyStrategy"
  durable_writes    = true

  region_replication_factors = {
    ap-southeast-2 = "3"
    us-east-1      = "2"
    eu-west-1      = "2"
  }
}

resource "cassandra_system_level_profile" "default" {
  name    = "default"
  comment = "Balanced baseline for general-purpose tables"

  compaction = {
    class = "org.apache.cassandra.db.compaction.UnifiedCompactionStrategy"
  }

  gc_grace_seconds = 86400
}

resource "cassandra_system_level_profile" "read_heavy" {
  name    = "read_heavy"
  comment = "Read-optimized baseline for latency-sensitive workloads"

  compaction = {
    class = "org.apache.cassandra.db.compaction.LeveledCompactionStrategy"
  }

  gc_grace_seconds = 86400
}

resource "cassandra_system_level_profile" "write_heavy" {
  name = "write_heavy"
  comment = "Write-optimized baseline for high-ingest workloads"

  compaction = {
    class = "org.apache.cassandra.db.compaction.TimeWindowCompactionStrategy"
    options = {
      compaction_window_size = "1"
      compaction_window_unit = "DAYS"
    }
  }

  gc_grace_seconds = 86400
  comment          = "Write-heavy profile managed by DB admin Terraform"

  additional_options = {
    caching = "{'keys':'ALL','rows_per_partition':'NONE'}"
  }
}

resource "cassandra_system_level_table_settings" "events_storage" {
  keyspace   = "app"
  table_name = "events"

  additional_options = {
    compression = "{'class':'LZ4Compressor','chunk_length_in_kb':'64'}"
  }
}
