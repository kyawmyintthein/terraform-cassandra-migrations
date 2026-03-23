package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

type CassandraClientConfig struct {
	Hosts                 []string
	Port                  int
	LocalDatacenter       string
	Username              string
	Password              string
	Consistency           string
	TimeoutSeconds        int
	MigrationLockKeyspace string
	MigrationLockTable    string
}

type CassandraClient struct {
	session               *gocql.Session
	migrationLockKeyspace string
	migrationLockTable    string
}

const (
	profileRegistryKeyspace = "terraform_schema_migration"
	profileRegistryTable    = "system_level_profiles"
	schemaLockTable         = "schema_migration_locks"
	schemaLockTTLSeconds    = 90
	schemaLockRetryInterval = 2 * time.Second
)

type SchemaMigrationLock struct {
	resourceID string
	token      string
}

func NewCassandraClient(config CassandraClientConfig) (*CassandraClient, error) {
	cluster := gocql.NewCluster(config.Hosts...)
	cluster.Port = config.Port
	cluster.Consistency = parseConsistency(config.Consistency)
	cluster.Timeout = time.Duration(config.TimeoutSeconds) * time.Second
	cluster.ProtoVersion = 4
	cluster.DisableInitialHostLookup = false
	cluster.PoolConfig.HostSelectionPolicy = gocql.DCAwareRoundRobinPolicy(config.LocalDatacenter)

	if config.Username != "" || config.Password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: config.Username,
			Password: config.Password,
		}
	}

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, err
	}

	return &CassandraClient{
		session:               session,
		migrationLockKeyspace: config.MigrationLockKeyspace,
		migrationLockTable:    config.MigrationLockTable,
	}, nil
}

func (c *CassandraClient) Close() {
	if c != nil && c.session != nil {
		c.session.Close()
	}
}

func (c *CassandraClient) Exec(statement string) error {
	return c.session.Query(statement).Exec()
}

func (c *CassandraClient) ExecSchemaMutation(ctx context.Context, statement string) error {
	if err := c.session.Query(statement).WithContext(ctx).Exec(); err != nil {
		return err
	}
	return c.session.AwaitSchemaAgreement(ctx)
}

func (c *CassandraClient) TableExists(keyspace, table string) (bool, error) {
	var found string
	err := c.session.Query(
		`SELECT table_name FROM system_schema.tables WHERE keyspace_name = ? AND table_name = ? LIMIT 1`,
		keyspace, table,
	).Scan(&found)
	if err == gocql.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *CassandraClient) IndexExists(keyspace, indexName string) (bool, error) {
	var found string
	err := c.session.Query(
		`SELECT index_name FROM system_schema.indexes WHERE keyspace_name = ? AND index_name = ? LIMIT 1`,
		keyspace, indexName,
	).Scan(&found)
	if err == gocql.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *CassandraClient) EnsureProfileStore() error {
	keyspaceStmt := fmt.Sprintf(
		"CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}",
		quoteIdentifier(profileRegistryKeyspace),
	)
	if err := c.ExecSchemaMutation(context.Background(), keyspaceStmt); err != nil {
		return err
	}

	tableStmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s.%s (profile_name text PRIMARY KEY, payload text, updated_at timestamp)",
		quoteIdentifier(profileRegistryKeyspace),
		quoteIdentifier(profileRegistryTable),
	)
	return c.ExecSchemaMutation(context.Background(), tableStmt)
}

func (c *CassandraClient) SchemaLockStoreExists() (bool, error) {
	if strings.TrimSpace(c.migrationLockKeyspace) == "" || strings.TrimSpace(c.migrationLockTable) == "" {
		return false, fmt.Errorf("migration lock keyspace and table must be configured in the provider")
	}

	return c.TableExists(c.migrationLockKeyspace, c.migrationLockTable)
}

func (c *CassandraClient) WithSchemaMigrationLock(ctx context.Context, resourceID, operation string, fn func(context.Context) error) error {
	lock, err := c.AcquireSchemaMigrationLock(ctx, resourceID, operation)
	if err != nil {
		return err
	}
	defer func() {
		_ = c.ReleaseSchemaMigrationLock(context.Background(), lock)
	}()

	renewCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	renewErrCh := make(chan error, 1)
	go func() {
		ticker := time.NewTicker((schemaLockTTLSeconds * time.Second) / 3)
		defer ticker.Stop()

		for {
			select {
			case <-renewCtx.Done():
				renewErrCh <- nil
				return
			case <-ticker.C:
				if err := c.RenewSchemaMigrationLock(renewCtx, lock, operation); err != nil {
					renewErrCh <- err
					return
				}
			}
		}
	}()

	fnErr := fn(ctx)
	cancel()
	renewErr := <-renewErrCh

	if fnErr != nil {
		return fnErr
	}
	if renewErr != nil {
		return renewErr
	}
	return nil
}

func (c *CassandraClient) AcquireSchemaMigrationLock(ctx context.Context, resourceID, operation string) (SchemaMigrationLock, error) {
	exists, err := c.SchemaLockStoreExists()
	if err != nil {
		return SchemaMigrationLock{}, err
	}
	if !exists {
		return SchemaMigrationLock{}, fmt.Errorf("schema migration lock store %s.%s was not found. A platform team must provision it with cassandra_system_level_migration_lock_store before user-level schema migrations can run", c.migrationLockKeyspace, c.migrationLockTable)
	}

	token, err := newLockToken()
	if err != nil {
		return SchemaMigrationLock{}, err
	}

	owner, err := schemaLockOwner()
	if err != nil {
		return SchemaMigrationLock{}, err
	}

	for {
		now := time.Now().UTC()
		leaseExpiresAt := now.Add(schemaLockTTLSeconds * time.Second)
		query := c.session.Query(
			fmt.Sprintf(
				"INSERT INTO %s.%s (resource_id, lock_token, owner, operation, started_at, last_heartbeat, lease_expires_at) VALUES (?, ?, ?, ?, ?, ?, ?) USING TTL ? IF NOT EXISTS",
				quoteIdentifier(c.migrationLockKeyspace),
				quoteIdentifier(c.migrationLockTable),
			),
			resourceID,
			token,
			owner,
			operation,
			now,
			now,
			leaseExpiresAt,
			schemaLockTTLSeconds,
		).WithContext(ctx).SerialConsistency(gocql.LocalSerial)

		existing := map[string]interface{}{}
		applied, err := query.MapScanCAS(existing)
		if err != nil {
			return SchemaMigrationLock{}, err
		}
		if applied {
			return SchemaMigrationLock{resourceID: resourceID, token: token}, nil
		}

		leaseExpired, err := existingLeaseExpired(existing, now)
		if err != nil {
			return SchemaMigrationLock{}, err
		}
		if leaseExpired {
			applied, err := c.session.Query(
				fmt.Sprintf(
					"UPDATE %s.%s USING TTL ? SET lock_token = ?, owner = ?, operation = ?, started_at = ?, last_heartbeat = ?, lease_expires_at = ? WHERE resource_id = ? IF lease_expires_at < ?",
					quoteIdentifier(c.migrationLockKeyspace),
					quoteIdentifier(c.migrationLockTable),
				),
				schemaLockTTLSeconds,
				token,
				owner,
				operation,
				now,
				now,
				leaseExpiresAt,
				resourceID,
				now,
			).WithContext(ctx).SerialConsistency(gocql.LocalSerial).ScanCAS()
			if err != nil {
				return SchemaMigrationLock{}, err
			}
			if applied {
				return SchemaMigrationLock{resourceID: resourceID, token: token}, nil
			}
		}

		select {
		case <-ctx.Done():
			return SchemaMigrationLock{}, fmt.Errorf("timed out waiting for schema migration lock on %q: %w", resourceID, ctx.Err())
		case <-time.After(schemaLockRetryInterval):
		}
	}
}

func (c *CassandraClient) RenewSchemaMigrationLock(ctx context.Context, lock SchemaMigrationLock, operation string) error {
	now := time.Now().UTC()
	leaseExpiresAt := now.Add(schemaLockTTLSeconds * time.Second)
	owner, err := schemaLockOwner()
	if err != nil {
		return err
	}

	applied, err := c.session.Query(
		fmt.Sprintf(
			"UPDATE %s.%s USING TTL ? SET owner = ?, operation = ?, last_heartbeat = ?, lease_expires_at = ? WHERE resource_id = ? IF lock_token = ?",
			quoteIdentifier(c.migrationLockKeyspace),
			quoteIdentifier(c.migrationLockTable),
		),
		schemaLockTTLSeconds,
		owner,
		operation,
		now,
		leaseExpiresAt,
		lock.resourceID,
		lock.token,
	).WithContext(ctx).SerialConsistency(gocql.LocalSerial).ScanCAS()
	if err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("schema migration lock for %q was lost while migration was still running", lock.resourceID)
	}
	return nil
}

func (c *CassandraClient) ReleaseSchemaMigrationLock(ctx context.Context, lock SchemaMigrationLock) error {
	applied, err := c.session.Query(
		fmt.Sprintf(
			"DELETE FROM %s.%s WHERE resource_id = ? IF lock_token = ?",
			quoteIdentifier(c.migrationLockKeyspace),
			quoteIdentifier(c.migrationLockTable),
		),
		lock.resourceID,
		lock.token,
	).WithContext(ctx).SerialConsistency(gocql.LocalSerial).ScanCAS()
	if err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("schema migration lock for %q could not be released because ownership changed", lock.resourceID)
	}
	return nil
}

func (c *CassandraClient) UpsertSystemProfile(name string, settings SystemSettings) error {
	if err := c.EnsureProfileStore(); err != nil {
		return err
	}

	payload, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	return c.session.Query(
		fmt.Sprintf(
			"INSERT INTO %s.%s (profile_name, payload, updated_at) VALUES (?, ?, toTimestamp(now()))",
			quoteIdentifier(profileRegistryKeyspace),
			quoteIdentifier(profileRegistryTable),
		),
		name,
		string(payload),
	).Exec()
}

func (c *CassandraClient) GetSystemProfile(name string) (SystemSettings, bool, error) {
	if err := c.EnsureProfileStore(); err != nil {
		return SystemSettings{}, false, err
	}

	var payload string
	err := c.session.Query(
		fmt.Sprintf(
			"SELECT payload FROM %s.%s WHERE profile_name = ? LIMIT 1",
			quoteIdentifier(profileRegistryKeyspace),
			quoteIdentifier(profileRegistryTable),
		),
		name,
	).Scan(&payload)
	if err == gocql.ErrNotFound {
		return SystemSettings{}, false, nil
	}
	if err != nil {
		return SystemSettings{}, false, err
	}

	var settings SystemSettings
	if err := json.Unmarshal([]byte(payload), &settings); err != nil {
		return SystemSettings{}, false, err
	}
	return settings, true, nil
}

func (c *CassandraClient) DeleteSystemProfile(name string) error {
	if err := c.EnsureProfileStore(); err != nil {
		return err
	}
	return c.session.Query(
		fmt.Sprintf(
			"DELETE FROM %s.%s WHERE profile_name = ?",
			quoteIdentifier(profileRegistryKeyspace),
			quoteIdentifier(profileRegistryTable),
		),
		name,
	).Exec()
}

func parseConsistency(raw string) gocql.Consistency {
	switch strings.ToUpper(raw) {
	case "ANY":
		return gocql.Any
	case "ONE":
		return gocql.One
	case "TWO":
		return gocql.Two
	case "THREE":
		return gocql.Three
	case "ALL":
		return gocql.All
	case "LOCAL_QUORUM":
		return gocql.LocalQuorum
	case "EACH_QUORUM":
		return gocql.EachQuorum
	case "LOCAL_ONE":
		return gocql.LocalOne
	default:
		return gocql.Quorum
	}
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func qualifiedTableName(keyspace, table string) string {
	return fmt.Sprintf("%s.%s", quoteIdentifier(keyspace), quoteIdentifier(table))
}

func quoteStringLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func newLockToken() (string, error) {
	token, err := gocql.RandomUUID()
	if err != nil {
		return "", err
	}
	return token.String(), nil
}

func schemaLockOwner() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", hostname, os.Getpid()), nil
}

func existingLeaseExpired(existing map[string]interface{}, now time.Time) (bool, error) {
	value, ok := existing["lease_expires_at"]
	if !ok || value == nil {
		return false, nil
	}

	switch typed := value.(type) {
	case time.Time:
		return !typed.After(now), nil
	case *time.Time:
		if typed == nil {
			return false, nil
		}
		return !typed.After(now), nil
	default:
		return false, fmt.Errorf("unexpected lease_expires_at type %T in schema migration lock row", value)
	}
}
