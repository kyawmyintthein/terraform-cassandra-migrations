terraform {
  required_providers {
    cassandra = {
      source = "kyawmyintthein/cassandra"
    }
  }
}

provider "cassandra" {
  hosts            = ["127.0.0.1"]
  port             = 9042
  local_datacenter = "dc1"
}

resource "cassandra_system_level_keyspace_policy" "regional" {
  name = "regional"

  replication_class = "NetworkTopologyStrategy"

  region_replication_factors = {
    dc1 = "3"
    dc2 = "2"
  }
}

resource "cassandra_user_level_keyspace" "app" {
  keyspace                        = "app"
  required_system_keyspace_policy = cassandra_system_level_keyspace_policy.regional.name
  regions                         = ["dc1", "dc2"]
}

resource "cassandra_user_level_table" "events" {
  depends_on = [cassandra_user_level_keyspace.app]

  keyspace                = "app"
  table_name              = "events"
  if_not_exists           = true
  required_system_profile = cassandra_system_level_profile.write_heavy.name

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

  compaction = {
    class = "org.apache.cassandra.db.compaction.TimeWindowCompactionStrategy"
    options = {
      compaction_window_size = "1"
      compaction_window_unit = "DAYS"
    }
  }

  gc_grace_seconds = 86400
  comment          = "Managed by central schema review"
}
