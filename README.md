# Cassandra Schema Migration Terraform Provider

This provider manages Cassandra schema changes in two layers:

- `cassandra_user_level_keyspace`: app-owned keyspace requests that choose only the keyspace name and approved regions or datacenters.
- `cassandra_user_level_table`: table shape, keys, additive/removal column migrations, and SAI indexes.
- `cassandra_system_level_keyspace_policy`: admin-managed keyspace replication policy and per-region replica settings.
- `cassandra_system_level_table_settings`: operational table settings such as compaction strategy and table options.

## Project status

This repository is set up as an open source project with contribution, security, and review guidance for collaborators.

See:

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
- [SECURITY.md](SECURITY.md)

## Recommended operating model

Recommended default: split ownership into two Terraform projects and two Terraform states.

- DB admin or platform team owns system-level keyspace policies, table profiles, and exception settings.
- Client app team owns user-level keyspace requests and table definition.

This split reduces risk because application teams can evolve table shape without accidentally changing compaction or storage behavior, while DB admins keep control over operational tuning. It also solves the "half-created table" problem by letting the app team require an admin-managed system profile during table creation.

The same boundary now applies to keyspaces: platform teams define replication strategy, durable writes, and approved datacenters or regions once, while app teams only choose the keyspace name and which approved regions they need active.

Use the central PR-review model only when:

- one team already reviews every schema change
- the organization is small enough that review bottlenecks are acceptable
- you want one place for all schema governance more than you want team autonomy

## Repo examples

- Split ownership example for app teams: `examples/split-ownership/user-level`
- Split ownership example for DB admins: `examples/split-ownership/system-level`
- Central review example: `examples/central-review`
- AWS Secrets Manager auth example: `examples/external-secrets/aws-secrets-manager`
- Vault auth example: `examples/external-secrets/vault`

## Apply order

When you use separate Terraform states:

1. Apply the system-level profile Terraform first so approved profiles exist in Cassandra.
2. Apply the system-level keyspace policy Terraform first so approved keyspace policies exist before app teams request keyspaces.
3. Apply the user-level Terraform after that so the keyspace and table are created with approved system defaults already attached.
4. Optionally apply table-specific `cassandra_system_level_table_settings` afterward for one-off overrides.

The provider keeps responsibilities separate, but app teams can now require a DB-admin profile up front so production tables are not created without essential compaction and operational defaults.

## Migration coordination

Table-level schema mutations are serialized per Cassandra table through a platform-managed lock table. The provider acquires the lock with a Cassandra lightweight transaction, renews the lease while the migration is running, and waits for schema agreement after each schema-changing statement before continuing.

Provision the lock store first with the system-level resource:

```hcl
resource "cassandra_system_level_migration_lock_store" "schema" {
  keyspace   = "terraform_schema_migration"
  table_name = "schema_migration_locks"

  replication = {
    class = "NetworkTopologyStrategy"
    dc1   = "3"
  }
}
```

The `keyspace` and `table_name` must match the provider's `migration_lock_keyspace` and `migration_lock_table` settings used by applies that mutate Cassandra schema. Resources that rely on schema locking will not auto-create this store; they fail with a clear error until the platform-managed lock store exists.

This reduces drift when separate Terraform runs or pipelines try to mutate the same table concurrently, including overlap between user-level table changes and system-level per-table setting changes. It is still best practice to serialize Terraform applies per environment in CI/CD so Cassandra locking remains a safety net rather than the only coordination layer.

## Provider

```hcl
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
  system_metadata_keyspace = "terraform_schema_migration"
  system_metadata_replication = {
    class              = "NetworkTopologyStrategy"
    dc1                = "3"
    dc2                = "2"
  }
  migration_lock_keyspace  = "terraform_schema_migration"
  migration_lock_table     = "schema_migration_locks"
}
```

Unless you override it with `consistency` or `CASSANDRA_CONSISTENCY`, the provider uses `LOCAL_QUORUM` for schema operations by default. This aligns schema changes with the configured `local_datacenter` and avoids relying on cross-datacenter quorum by default.

`cassandra_system_level_profile` and `cassandra_system_level_keyspace_policy` automatically create their shared metadata keyspace if it does not exist yet. Use `system_metadata_keyspace` to place that store where your platform team wants it, and `system_metadata_replication` to control its replication strategy and replication factor explicitly.

The provider also supports environment-variable configuration, which is useful when CI/CD, Vault Agent, Kubernetes secrets, or another secret delivery system injects credentials at runtime:

- `CASSANDRA_HOSTS` as a comma-separated host list
- `CASSANDRA_PORT`
- `CASSANDRA_LOCAL_DATACENTER`
- `CASSANDRA_USERNAME`
- `CASSANDRA_PASSWORD`
- `CASSANDRA_CONSISTENCY`
- `CASSANDRA_TIMEOUT_SECONDS`
- `CASSANDRA_SYSTEM_METADATA_KEYSPACE`
- `CASSANDRA_MIGRATION_LOCK_KEYSPACE`
- `CASSANDRA_MIGRATION_LOCK_TABLE`

Example:

```bash
export CASSANDRA_HOSTS=10.0.0.10,10.0.0.11
export CASSANDRA_LOCAL_DATACENTER=ap-southeast-2
export CASSANDRA_USERNAME=$(vault kv get -field=username kv/cassandra/prod)
export CASSANDRA_PASSWORD=$(vault kv get -field=password kv/cassandra/prod)
terraform apply
```

You can still set `username` and `password` directly in the provider block when those values come from another Terraform provider or data source.

## External secret sources

The Cassandra provider does not need a built-in AWS or Vault integration. Terraform can resolve credentials from other providers and pass the resulting sensitive values into the Cassandra provider.

### AWS Secrets Manager

```hcl
terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
    cassandra = {
      source = "kyawmyintthein/cassandra"
    }
  }
}

data "aws_secretsmanager_secret_version" "cassandra" {
  secret_id = var.cassandra_secret_id
}

locals {
  cassandra_auth = jsondecode(data.aws_secretsmanager_secret_version.cassandra.secret_string)
}

provider "cassandra" {
  hosts            = ["127.0.0.1"]
  local_datacenter = "dc1"
  username         = local.cassandra_auth.username
  password         = local.cassandra_auth.password
}
```

Expected secret JSON:

```json
{
  "username": "schema_migrator",
  "password": "super-secret"
}
```

### Vault KV v2

```hcl
terraform {
  required_providers {
    vault = {
      source = "hashicorp/vault"
    }
    cassandra = {
      source = "kyawmyintthein/cassandra"
    }
  }
}

data "vault_kv_secret_v2" "cassandra" {
  mount = var.vault_mount
  name  = var.vault_secret_name
}

provider "cassandra" {
  hosts            = ["127.0.0.1"]
  local_datacenter = "dc1"
  username         = data.vault_kv_secret_v2.cassandra.data["username"]
  password         = data.vault_kv_secret_v2.cassandra.data["password"]
}
```

This pattern also works with any other secret source that can expose credentials as Terraform expressions or environment variables.

## Admin-managed profiles

```hcl
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
  comment          = "Write-heavy production profile"
}
```

Use these profiles as shared operational baselines. Keep common defaults such as compaction strategy in the profile, and keep table-specific tuning such as compression `chunk_length_in_kb` in `cassandra_system_level_table_settings`.

## Admin-managed keyspace policies

```hcl
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
```

Use a keyspace policy to hide replication strategy details from application teams. With `NetworkTopologyStrategy`, the platform team defines the allowed region or datacenter names and each region's replication factor in `region_replication_factors`; the app team selects only which of those approved regions it wants active. With `SimpleStrategy`, the app team does not provide any regions and the platform team uses `replication_factor`.

## User-level keyspaces

```hcl
resource "cassandra_user_level_keyspace" "app" {
  keyspace                        = "app"
  if_not_exists                   = true
  required_system_keyspace_policy = "regional"
  regions                         = ["ap-southeast-2", "us-east-1"]
}
```

This resource intentionally limits the app-owned surface area:

- app teams choose the keyspace name
- app teams choose which approved regions or datacenters are active
- platform teams keep ownership of replication strategy, per-region replica counts, and durable writes

For safety, changing `required_system_keyspace_policy` is create-time only, and removing an already-managed region is rejected for in-place updates so replica reductions stay explicit and manually coordinated.

## User-level schema

```hcl
resource "cassandra_user_level_table" "events" {
  depends_on = [cassandra_user_level_keyspace.app]

  keyspace                = "app"
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
```

## System-level settings overrides

```hcl
resource "cassandra_system_level_table_settings" "events" {
  keyspace   = "app"
  table_name = "events"

  additional_options = {
    compression = "{'class':'LZ4Compressor','chunk_length_in_kb':'64'}"
  }
}
```

## Ownership guidance

Recommended ownership split:

- `cassandra_user_level_keyspace`: app or service team
- `cassandra_user_level_table`: app or service team
- `cassandra_system_level_keyspace_policy`: DB admin or platform team
- `cassandra_system_level_profile`: DB admin or platform team
- `cassandra_system_level_table_settings`: DB admin or platform team

Avoid having two Terraform states manage the same concern. A clean boundary is:

- user-level owns columns, primary key layout, clustering order, SAI definitions, and the choice of approved profile
- user-level keyspace owns only the requested keyspace name and the selected approved regions
- system-level keyspace policy owns replication strategy, durable writes, and per-region replica guardrails
- system-level profile owns shared defaults such as compaction and general operational policy
- system-level table settings owns table-specific exceptions only

## Behavior notes

- Partition keys and clustering keys are treated as immutable.
- Column type changes are rejected because Cassandra does not safely support all in-place type migrations.
- `required_system_keyspace_policy` is create-time only. Region additions are allowed, but region removal is rejected for in-place updates.
- `required_system_profile` is create-time only. Change per-table operational tuning later with `cassandra_system_level_table_settings`.
- Resource deletion removes Terraform state only; it does not drop live keyspaces, tables, or indexes.

## Development

Local checks:

```bash
gofmt -w .
go vet ./...
go test ./...
```
