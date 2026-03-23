package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

type CassandraClientConfig struct {
	Hosts           []string
	Port            int
	LocalDatacenter string
	Username        string
	Password        string
	Consistency     string
	TimeoutSeconds  int
}

type CassandraClient struct {
	session *gocql.Session
}

const (
	profileRegistryKeyspace = "terraform_schema_migration"
	profileRegistryTable    = "system_level_profiles"
)

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

	return &CassandraClient{session: session}, nil
}

func (c *CassandraClient) Close() {
	if c != nil && c.session != nil {
		c.session.Close()
	}
}

func (c *CassandraClient) Exec(statement string) error {
	return c.session.Query(statement).Exec()
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
	if err := c.Exec(keyspaceStmt); err != nil {
		return err
	}

	tableStmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s.%s (profile_name text PRIMARY KEY, payload text, updated_at timestamp)",
		quoteIdentifier(profileRegistryKeyspace),
		quoteIdentifier(profileRegistryTable),
	)
	return c.Exec(tableStmt)
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
