package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = (*UserLevelKeyspaceResource)(nil)
var _ resource.ResourceWithConfigure = (*UserLevelKeyspaceResource)(nil)
var _ resource.ResourceWithImportState = (*UserLevelKeyspaceResource)(nil)

type UserLevelKeyspaceResource struct {
	client *CassandraClient
}

type UserLevelKeyspaceModel struct {
	ID                           types.String `tfsdk:"id"`
	Keyspace                     types.String `tfsdk:"keyspace"`
	IfNotExists                  types.Bool   `tfsdk:"if_not_exists"`
	RequiredSystemKeyspacePolicy types.String `tfsdk:"required_system_keyspace_policy"`
	Regions                      types.List   `tfsdk:"regions"`
}

func NewUserLevelKeyspaceResource() resource.Resource {
	return &UserLevelKeyspaceResource{}
}

func (r *UserLevelKeyspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user_level_keyspace"
}

func (r *UserLevelKeyspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Defines an app-team-managed Cassandra keyspace request. The app team chooses the keyspace name and the regions or datacenters to activate, while a required system keyspace policy supplies replication strategy and durable_writes defaults.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"keyspace": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"if_not_exists": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"required_system_keyspace_policy": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the DB-admin managed cassandra_system_level_keyspace_policy that governs replication settings for this keyspace.",
			},
			"regions": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Datacenters or regions to activate for this keyspace when the selected policy uses NetworkTopologyStrategy.",
				Validators: []validator.List{
					listvalidator.UniqueValues(),
				},
			},
		},
	}
}

func (r *UserLevelKeyspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*CassandraClient)
}

func (r *UserLevelKeyspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan UserLevelKeyspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.applyKeyspace(ctx, plan, true, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(plan.Keyspace.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserLevelKeyspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UserLevelKeyspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	exists, err := r.client.KeyspaceExists(state.Keyspace.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Keyspace", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
	}
}

func (r *UserLevelKeyspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan UserLevelKeyspaceModel
	var state UserLevelKeyspaceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.RequiredSystemKeyspacePolicy.ValueString() != state.RequiredSystemKeyspacePolicy.ValueString() {
		resp.Diagnostics.AddAttributeError(
			path.Root("required_system_keyspace_policy"),
			"Required System Keyspace Policy Is Create-Time Only",
			"Changing required_system_keyspace_policy after keyspace creation is not supported. Keep replication strategy decisions in the original admin-managed policy and only make additive region changes from the app-owned resource.",
		)
		return
	}

	previousRegions, err := decodeRegions(ctx, state.Regions)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("regions"), "Invalid Regions", err.Error())
		return
	}
	nextRegions, err := decodeRegions(ctx, plan.Regions)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("regions"), "Invalid Regions", err.Error())
		return
	}
	if err := validateNoRegionRemoval(previousRegions, nextRegions); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("regions"), "Region Removal Is Not Supported In-Place", err.Error())
		return
	}

	r.applyKeyspace(ctx, plan, false, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = types.StringValue(plan.Keyspace.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserLevelKeyspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.State.RemoveResource(ctx)
}

func (r *UserLevelKeyspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *UserLevelKeyspaceResource) applyKeyspace(ctx context.Context, plan UserLevelKeyspaceModel, creating bool, diags *diag.Diagnostics) {
	policy, exists, err := r.client.GetSystemKeyspacePolicy(plan.RequiredSystemKeyspacePolicy.ValueString())
	if err != nil {
		diags.AddError("Unable to Resolve Required System Keyspace Policy", err.Error())
		return
	}
	if !exists {
		attrPath, summary, detail := missingSystemKeyspacePolicyDiagnostic(plan.RequiredSystemKeyspacePolicy.ValueString())
		diags.AddAttributeError(attrPath, summary, detail)
		return
	}

	regions, err := decodeRegions(ctx, plan.Regions)
	if err != nil {
		diags.AddAttributeError(path.Root("regions"), "Invalid Regions", err.Error())
		return
	}

	replication, err := buildReplicationFromPolicy(policy, regions)
	if err != nil {
		diags.AddAttributeError(path.Root("regions"), "Invalid Keyspace Region Selection", err.Error())
		return
	}

	durableWrites := true
	if policy.DurableWrites != nil {
		durableWrites = *policy.DurableWrites
	}

	statement := buildAlterKeyspaceStatement(plan.Keyspace.ValueString(), replication, durableWrites)
	operation := "update keyspace"
	if creating {
		statement = buildCreateKeyspaceStatement(plan.Keyspace.ValueString(), plan.IfNotExists.ValueBool(), replication, durableWrites)
		operation = "create keyspace"
	}

	resourceID := "keyspace." + plan.Keyspace.ValueString()
	if err := r.client.WithSchemaMigrationLock(ctx, resourceID, operation, func(lockCtx context.Context) error {
		return r.client.ExecSchemaMutation(lockCtx, statement)
	}); err != nil {
		diags.AddError("Unable to Apply Keyspace Configuration", err.Error())
		return
	}
}
