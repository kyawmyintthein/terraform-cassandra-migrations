package provider

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = (*CassandraProvider)(nil)

func New() provider.Provider {
	return &CassandraProvider{}
}

type CassandraProvider struct{}

type CassandraProviderModel struct {
	Hosts                 types.List   `tfsdk:"hosts"`
	Port                  types.Int64  `tfsdk:"port"`
	LocalDatacenter       types.String `tfsdk:"local_datacenter"`
	Username              types.String `tfsdk:"username"`
	Password              types.String `tfsdk:"password"`
	Consistency           types.String `tfsdk:"consistency"`
	TimeoutSeconds        types.Int64  `tfsdk:"timeout_seconds"`
	MigrationLockKeyspace types.String `tfsdk:"migration_lock_keyspace"`
	MigrationLockTable    types.String `tfsdk:"migration_lock_table"`
}

func (p *CassandraProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "cassandra"
}

func (p *CassandraProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Terraform provider for Cassandra schema migrations with user-level and system-level resources.",
		Attributes: map[string]schema.Attribute{
			"hosts": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Seed Cassandra hosts. Can also be set with CASSANDRA_HOSTS as a comma-separated list.",
			},
			"port": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Cassandra native transport port. Defaults to 9042 or CASSANDRA_PORT when set.",
			},
			"local_datacenter": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Local datacenter for token-aware routing. Can also be set with CASSANDRA_LOCAL_DATACENTER.",
			},
			"username": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional username. Can also be set with CASSANDRA_USERNAME.",
			},
			"password": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Optional password. Can also be set with CASSANDRA_PASSWORD.",
			},
			"consistency": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Consistency level for schema operations. Defaults to QUORUM or CASSANDRA_CONSISTENCY when set.",
			},
			"timeout_seconds": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Query timeout in seconds. Defaults to 30 or CASSANDRA_TIMEOUT_SECONDS when set.",
			},
			"migration_lock_keyspace": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Platform-managed keyspace that stores schema migration locks. Defaults to terraform_schema_migration.",
			},
			"migration_lock_table": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Platform-managed table that stores schema migration locks. Defaults to schema_migration_locks.",
			},
		},
	}
}

func (p *CassandraProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config CassandraProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Hosts.IsUnknown() ||
		config.Port.IsUnknown() ||
		config.LocalDatacenter.IsUnknown() ||
		config.Username.IsUnknown() ||
		config.Password.IsUnknown() ||
		config.Consistency.IsUnknown() ||
		config.TimeoutSeconds.IsUnknown() ||
		config.MigrationLockKeyspace.IsUnknown() ||
		config.MigrationLockTable.IsUnknown() {
		return
	}

	hosts := readHostsConfig(ctx, config, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	if len(hosts) == 0 {
		resp.Diagnostics.AddAttributeError(
			path.Root("hosts"),
			"Missing Hosts",
			"At least one Cassandra host must be configured either in the provider block or with CASSANDRA_HOSTS.",
		)
		return
	}

	localDatacenter := readStringConfig(config.LocalDatacenter, "CASSANDRA_LOCAL_DATACENTER")
	if localDatacenter == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("local_datacenter"),
			"Missing Local Datacenter",
			"A Cassandra local datacenter must be configured either in the provider block or with CASSANDRA_LOCAL_DATACENTER.",
		)
		return
	}

	clientConfig := CassandraClientConfig{
		Hosts:                 hosts,
		Port:                  readIntConfig(config.Port, "CASSANDRA_PORT", 9042, &resp.Diagnostics),
		LocalDatacenter:       localDatacenter,
		Username:              readStringConfig(config.Username, "CASSANDRA_USERNAME"),
		Password:              readStringConfig(config.Password, "CASSANDRA_PASSWORD"),
		Consistency:           readStringConfigWithDefault(config.Consistency, "CASSANDRA_CONSISTENCY", "QUORUM"),
		TimeoutSeconds:        readIntConfig(config.TimeoutSeconds, "CASSANDRA_TIMEOUT_SECONDS", 30, &resp.Diagnostics),
		MigrationLockKeyspace: readStringConfigWithDefault(config.MigrationLockKeyspace, "CASSANDRA_MIGRATION_LOCK_KEYSPACE", profileRegistryKeyspace),
		MigrationLockTable:    readStringConfigWithDefault(config.MigrationLockTable, "CASSANDRA_MIGRATION_LOCK_TABLE", schemaLockTable),
	}
	if resp.Diagnostics.HasError() {
		return
	}

	client, err := NewCassandraClient(clientConfig)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Configure Cassandra Client",
			fmt.Sprintf("Failed to initialize Cassandra client: %s", err),
		)
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *CassandraProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewUserLevelTableResource,
		NewSystemLevelTableSettingsResource,
		NewSystemLevelProfileResource,
		NewSystemLevelMigrationLockStoreResource,
	}
}

func (p *CassandraProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

func readHostsConfig(ctx context.Context, config CassandraProviderModel, resp *provider.ConfigureResponse) []string {
	if !config.Hosts.IsNull() {
		var hosts []string
		resp.Diagnostics.Append(config.Hosts.ElementsAs(ctx, &hosts, false)...)
		if resp.Diagnostics.HasError() {
			return nil
		}
		return hosts
	}

	rawHosts := strings.TrimSpace(os.Getenv("CASSANDRA_HOSTS"))
	if rawHosts == "" {
		return nil
	}

	parts := strings.Split(rawHosts, ",")
	hosts := make([]string, 0, len(parts))
	for _, part := range parts {
		host := strings.TrimSpace(part)
		if host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func readStringConfig(value types.String, envName string) string {
	if !value.IsNull() {
		return value.ValueString()
	}
	return strings.TrimSpace(os.Getenv(envName))
}

func readStringConfigWithDefault(value types.String, envName, defaultValue string) string {
	resolved := readStringConfig(value, envName)
	if resolved == "" {
		return defaultValue
	}
	return resolved
}

func readIntConfig(value types.Int64, envName string, defaultValue int, diags *diag.Diagnostics) int {
	if !value.IsNull() {
		return int(value.ValueInt64())
	}

	raw := strings.TrimSpace(os.Getenv(envName))
	if raw == "" {
		return defaultValue
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		diags.AddError(
			"Invalid Environment Variable",
			fmt.Sprintf("%s must be a whole number, got %q.", envName, raw),
		)
		return defaultValue
	}

	return parsed
}
