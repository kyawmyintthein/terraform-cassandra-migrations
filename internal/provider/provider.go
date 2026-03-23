package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
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
	Hosts           types.List   `tfsdk:"hosts"`
	Port            types.Int64  `tfsdk:"port"`
	LocalDatacenter types.String `tfsdk:"local_datacenter"`
	Username        types.String `tfsdk:"username"`
	Password        types.String `tfsdk:"password"`
	Consistency     types.String `tfsdk:"consistency"`
	TimeoutSeconds  types.Int64  `tfsdk:"timeout_seconds"`
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
				Required:            true,
				MarkdownDescription: "Seed Cassandra hosts.",
			},
			"port": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Cassandra native transport port.",
			},
			"local_datacenter": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Local datacenter for token-aware routing.",
			},
			"username": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional username.",
			},
			"password": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Optional password.",
			},
			"consistency": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Consistency level for schema operations. Defaults to QUORUM.",
			},
			"timeout_seconds": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Query timeout in seconds. Defaults to 30.",
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

	if config.Hosts.IsUnknown() || config.LocalDatacenter.IsUnknown() {
		return
	}

	var hosts []string
	resp.Diagnostics.Append(config.Hosts.ElementsAs(ctx, &hosts, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if len(hosts) == 0 {
		resp.Diagnostics.AddAttributeError(
			path.Root("hosts"),
			"Missing Hosts",
			"At least one Cassandra host must be configured.",
		)
		return
	}

	clientConfig := CassandraClientConfig{
		Hosts:           hosts,
		Port:            9042,
		LocalDatacenter: config.LocalDatacenter.ValueString(),
		Consistency:     "QUORUM",
		TimeoutSeconds:  30,
	}

	if !config.Port.IsNull() {
		clientConfig.Port = int(config.Port.ValueInt64())
	}
	if !config.Username.IsNull() {
		clientConfig.Username = config.Username.ValueString()
	}
	if !config.Password.IsNull() {
		clientConfig.Password = config.Password.ValueString()
	}
	if !config.Consistency.IsNull() {
		clientConfig.Consistency = config.Consistency.ValueString()
	}
	if !config.TimeoutSeconds.IsNull() {
		clientConfig.TimeoutSeconds = int(config.TimeoutSeconds.ValueInt64())
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
	}
}

func (p *CassandraProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
