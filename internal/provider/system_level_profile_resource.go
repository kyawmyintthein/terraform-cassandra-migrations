package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = (*SystemLevelProfileResource)(nil)
var _ resource.ResourceWithConfigure = (*SystemLevelProfileResource)(nil)
var _ resource.ResourceWithImportState = (*SystemLevelProfileResource)(nil)

type SystemLevelProfileResource struct {
	client *CassandraClient
}

type SystemLevelProfileModel struct {
	ID                  types.String  `tfsdk:"id"`
	Name                types.String  `tfsdk:"name"`
	Comment             types.String  `tfsdk:"comment"`
	GCGraceSeconds      types.Int64   `tfsdk:"gc_grace_seconds"`
	DefaultTTL          types.Int64   `tfsdk:"default_time_to_live"`
	SpeculativeRetry    types.String  `tfsdk:"speculative_retry"`
	BloomFilterFPChance types.Float64 `tfsdk:"bloom_filter_fp_chance"`
	Compaction          types.Object  `tfsdk:"compaction"`
	AdditionalOptions   types.Map     `tfsdk:"additional_options"`
}

func NewSystemLevelProfileResource() resource.Resource {
	return &SystemLevelProfileResource{}
}

func (r *SystemLevelProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_system_level_profile"
}

func (r *SystemLevelProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Defines a reusable admin-managed system profile that can be referenced by app-owned table resources at creation time.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
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

func (r *SystemLevelProfileResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*CassandraClient)
}

func (r *SystemLevelProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SystemLevelProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	settings, err := extractProfileSettings(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Build Profile", err.Error())
		return
	}

	if err := r.client.UpsertSystemProfile(plan.Name.ValueString(), settings); err != nil {
		resp.Diagnostics.AddError("Unable to Store Profile", err.Error())
		return
	}

	plan.ID = plan.Name
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SystemLevelProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SystemLevelProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, exists, err := r.client.GetSystemProfile(state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Profile", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
	}
}

func (r *SystemLevelProfileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SystemLevelProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	settings, err := extractProfileSettings(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Build Profile", err.Error())
		return
	}

	if err := r.client.UpsertSystemProfile(plan.Name.ValueString(), settings); err != nil {
		resp.Diagnostics.AddError("Unable to Store Profile", err.Error())
		return
	}

	plan.ID = plan.Name
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SystemLevelProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SystemLevelProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteSystemProfile(state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to Delete Profile", err.Error())
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r *SystemLevelProfileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func extractProfileSettings(ctx context.Context, model SystemLevelProfileModel) (SystemSettings, error) {
	settingsModel := SystemLevelTableSettingsModel{
		Comment:             model.Comment,
		GCGraceSeconds:      model.GCGraceSeconds,
		DefaultTTL:          model.DefaultTTL,
		SpeculativeRetry:    model.SpeculativeRetry,
		BloomFilterFPChance: model.BloomFilterFPChance,
		Compaction:          model.Compaction,
		AdditionalOptions:   model.AdditionalOptions,
	}

	settings, err := extractSystemSettingsFromModel(ctx, settingsModel)
	if err != nil {
		return SystemSettings{}, err
	}

	if _, err := buildSystemSettingsClauses(settings); err != nil {
		return SystemSettings{}, fmt.Errorf("profile %q must define at least one system setting: %w", model.Name.ValueString(), err)
	}

	return settings, nil
}
