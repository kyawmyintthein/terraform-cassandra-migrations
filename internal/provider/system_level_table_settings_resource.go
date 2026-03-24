package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = (*SystemLevelTableSettingsResource)(nil)
var _ resource.ResourceWithConfigure = (*SystemLevelTableSettingsResource)(nil)
var _ resource.ResourceWithImportState = (*SystemLevelTableSettingsResource)(nil)

type SystemLevelTableSettingsResource struct {
	client *CassandraClient
}

type SystemLevelTableSettingsModel struct {
	ID                  types.String  `tfsdk:"id"`
	Keyspace            types.String  `tfsdk:"keyspace"`
	TableName           types.String  `tfsdk:"table_name"`
	Comment             types.String  `tfsdk:"comment"`
	GCGraceSeconds      types.Int64   `tfsdk:"gc_grace_seconds"`
	DefaultTTL          types.Int64   `tfsdk:"default_time_to_live"`
	SpeculativeRetry    types.String  `tfsdk:"speculative_retry"`
	BloomFilterFPChance types.Float64 `tfsdk:"bloom_filter_fp_chance"`
	Compaction          types.Object  `tfsdk:"compaction"`
	AdditionalOptions   types.Map     `tfsdk:"additional_options"`
}

type CompactionModel struct {
	Class   types.String `tfsdk:"class"`
	Options types.Map    `tfsdk:"options"`
}

func NewSystemLevelTableSettingsResource() resource.Resource {
	return &SystemLevelTableSettingsResource{}
}

func (r *SystemLevelTableSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_system_level_table_settings"
}

func (r *SystemLevelTableSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Defines system-level Cassandra table settings such as compaction strategy and table options.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"keyspace": schema.StringAttribute{
				Required: true,
			},
			"table_name": schema.StringAttribute{
				Required: true,
			},
			"comment": schema.StringAttribute{
				Optional: true,
			},
			"gc_grace_seconds": schema.Int64Attribute{
				Optional: true,
			},
			"default_time_to_live": schema.Int64Attribute{
				Optional: true,
			},
			"speculative_retry": schema.StringAttribute{
				Optional: true,
			},
			"bloom_filter_fp_chance": schema.Float64Attribute{
				Optional: true,
			},
			"compaction": schema.SingleNestedAttribute{
				Optional: true,
				Attributes: map[string]schema.Attribute{
					"class": schema.StringAttribute{
						Required: true,
					},
					"options": schema.MapAttribute{
						ElementType: types.StringType,
						Optional:    true,
					},
				},
			},
			"additional_options": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
			},
		},
	}
}

func (r *SystemLevelTableSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*CassandraClient)
}

func (r *SystemLevelTableSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SystemLevelTableSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.applySettings(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SystemLevelTableSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SystemLevelTableSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	exists, err := r.client.TableExists(state.Keyspace.ValueString(), state.TableName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Table Settings", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
	}
}

func (r *SystemLevelTableSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SystemLevelTableSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.applySettings(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SystemLevelTableSettingsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.State.RemoveResource(ctx)
}

func (r *SystemLevelTableSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *SystemLevelTableSettingsResource) applySettings(ctx context.Context, plan *SystemLevelTableSettingsModel, diags *diag.Diagnostics) {
	settings, err := extractSystemSettingsFromModel(ctx, *plan)
	if err != nil {
		diags.AddError("Unable to Build Table Settings Statement", err.Error())
		return
	}

	statement, err := buildAlterTableSettingsStatement(plan.Keyspace.ValueString(), plan.TableName.ValueString(), settings)
	if err != nil {
		diags.AddError("Unable to Build Table Settings Statement", err.Error())
		return
	}

	resourceID := plan.Keyspace.ValueString() + "." + plan.TableName.ValueString()
	if err := r.client.WithSchemaMigrationLock(ctx, resourceID, "alter table settings", func(lockCtx context.Context) error {
		return r.client.ExecSchemaMutation(lockCtx, statement)
	}); err != nil {
		diags.AddError("Unable to Apply Table Settings", fmt.Sprintf("CQL: %s\nError: %s", statement, err))
		return
	}

	plan.ID = types.StringValue(resourceID)
}
