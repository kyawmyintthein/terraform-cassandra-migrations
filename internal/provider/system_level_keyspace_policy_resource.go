package provider

import (
	"context"
	"fmt"

	_jsii "github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = (*SystemLevelKeyspacePolicyResource)(nil)
var _ resource.ResourceWithConfigure = (*SystemLevelKeyspacePolicyResource)(nil)
var _ resource.ResourceWithImportState = (*SystemLevelKeyspacePolicyResource)(nil)

type SystemLevelKeyspacePolicyResource struct {
	client *CassandraClient
}

type SystemLevelKeyspacePolicyModel struct {
	ID                       types.String `tfsdk:"id"`
	Name                     types.String `tfsdk:"name"`
	ReplicationClass         types.String `tfsdk:"replication_class"`
	ReplicationFactor        types.Int64  `tfsdk:"replication_factor"`
	RegionReplicationFactors types.Map    `tfsdk:"region_replication_factors"`
	DurableWrites            types.Bool   `tfsdk:"durable_writes"`
}

func NewSystemLevelKeyspacePolicyResource() resource.Resource {
	return &SystemLevelKeyspacePolicyResource{}
}

func (r *SystemLevelKeyspacePolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_system_level_keyspace_policy"
}

func (r *SystemLevelKeyspacePolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Defines an admin-managed keyspace policy that controls replication strategy, durable_writes, and which datacenters or regions app teams are allowed to target.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"replication_class": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					_jsii.OneOf(replicationClassSimpleStrategy, replicationClassNetworkTopologyStrategy),
				},
			},
			"replication_factor": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Keyspace replication factor for SimpleStrategy. Do not set this for NetworkTopologyStrategy.",
			},
			"region_replication_factors": schema.MapAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "For NetworkTopologyStrategy, maps each allowed datacenter or region to its replication factor, for example { ap-southeast-2 = \"3\", us-east-1 = \"2\" }.",
			},
			"durable_writes": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
		},
	}
}

func (r *SystemLevelKeyspacePolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*CassandraClient)
}

func (r *SystemLevelKeyspacePolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SystemLevelKeyspacePolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy, err := extractSystemKeyspacePolicy(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Build Keyspace Policy", err.Error())
		return
	}

	if err := r.client.UpsertSystemKeyspacePolicy(plan.Name.ValueString(), policy); err != nil {
		resp.Diagnostics.AddError("Unable to Store Keyspace Policy", err.Error())
		return
	}

	plan.ID = plan.Name
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SystemLevelKeyspacePolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SystemLevelKeyspacePolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, exists, err := r.client.GetSystemKeyspacePolicy(state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Keyspace Policy", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
	}
}

func (r *SystemLevelKeyspacePolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SystemLevelKeyspacePolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy, err := extractSystemKeyspacePolicy(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Build Keyspace Policy", err.Error())
		return
	}

	if err := r.client.UpsertSystemKeyspacePolicy(plan.Name.ValueString(), policy); err != nil {
		resp.Diagnostics.AddError("Unable to Store Keyspace Policy", err.Error())
		return
	}

	plan.ID = plan.Name
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SystemLevelKeyspacePolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SystemLevelKeyspacePolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteSystemKeyspacePolicy(state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to Delete Keyspace Policy", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *SystemLevelKeyspacePolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func extractSystemKeyspacePolicy(ctx context.Context, model SystemLevelKeyspacePolicyModel) (SystemKeyspacePolicy, error) {
	policy := SystemKeyspacePolicy{
		ReplicationClass: model.ReplicationClass.ValueString(),
	}

	if !model.ReplicationFactor.IsNull() && !model.ReplicationFactor.IsUnknown() {
		value := model.ReplicationFactor.ValueInt64()
		policy.ReplicationFactor = &value
	}
	if !model.RegionReplicationFactors.IsNull() && !model.RegionReplicationFactors.IsUnknown() {
		factors, err := decodeStringMap(ctx, model.RegionReplicationFactors)
		if err != nil {
			return SystemKeyspacePolicy{}, fmt.Errorf("unable to decode region_replication_factors: %w", err)
		}
		policy.RegionReplicationFactors = factors
	}
	if !model.DurableWrites.IsNull() && !model.DurableWrites.IsUnknown() {
		value := model.DurableWrites.ValueBool()
		policy.DurableWrites = &value
	}

	if err := validateSystemKeyspacePolicy(policy); err != nil {
		return SystemKeyspacePolicy{}, err
	}

	return policy, nil
}
