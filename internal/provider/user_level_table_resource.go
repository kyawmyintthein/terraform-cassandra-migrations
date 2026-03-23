package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	_jsii "github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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

var _ resource.Resource = (*UserLevelTableResource)(nil)
var _ resource.ResourceWithConfigure = (*UserLevelTableResource)(nil)
var _ resource.ResourceWithImportState = (*UserLevelTableResource)(nil)

type UserLevelTableResource struct {
	client *CassandraClient
}

type UserLevelTableModel struct {
	ID                    types.String `tfsdk:"id"`
	Keyspace              types.String `tfsdk:"keyspace"`
	TableName             types.String `tfsdk:"table_name"`
	IfNotExists           types.Bool   `tfsdk:"if_not_exists"`
	RequiredSystemProfile types.String `tfsdk:"required_system_profile"`
	Columns               types.List   `tfsdk:"columns"`
	PartitionKeys         types.List   `tfsdk:"partition_keys"`
	ClusteringKeys        types.List   `tfsdk:"clustering_keys"`
	SAIIndexes            types.List   `tfsdk:"sai_indexes"`
}

type ColumnModel struct {
	Name   types.String `tfsdk:"name"`
	Type   types.String `tfsdk:"type"`
	Static types.Bool   `tfsdk:"static"`
}

type ClusteringKeyModel struct {
	Name  types.String `tfsdk:"name"`
	Order types.String `tfsdk:"order"`
}

type SAIIndexModel struct {
	Name    types.String `tfsdk:"name"`
	Column  types.String `tfsdk:"column"`
	Options types.Map    `tfsdk:"options"`
}

func NewUserLevelTableResource() resource.Resource {
	return &UserLevelTableResource{}
}

func (r *UserLevelTableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user_level_table"
}

func (r *UserLevelTableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Defines user-level Cassandra schema mutations such as table creation, additive column changes, column removal, keys, and SAI indexes.",
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
			"if_not_exists": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"required_system_profile": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Name of a DB-admin managed cassandra_system_level_profile that must exist and will be applied during table creation.",
			},
			"columns": schema.ListNestedAttribute{
				Required: true,
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required: true,
						},
						"type": schema.StringAttribute{
							Required: true,
						},
						"static": schema.BoolAttribute{
							Optional: true,
							Computed: true,
							Default:  booldefault.StaticBool(false),
						},
					},
				},
			},
			"partition_keys": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
			},
			"clustering_keys": schema.ListNestedAttribute{
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required: true,
						},
						"order": schema.StringAttribute{
							Optional: true,
							Computed: true,
							Validators: []validator.String{
								_jsii.OneOf("ASC", "DESC", "asc", "desc"),
							},
						},
					},
				},
			},
			"sai_indexes": schema.ListNestedAttribute{
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required: true,
						},
						"column": schema.StringAttribute{
							Required: true,
						},
						"options": schema.MapAttribute{
							ElementType: types.StringType,
							Optional:    true,
						},
					},
				},
			},
		},
	}
}

func (r *UserLevelTableResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*CassandraClient)
}

func (r *UserLevelTableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan UserLevelTableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	columns, partitionKeys, clusteringKeys, saiIndexes, ok := decodeUserLevelValues(ctx, plan, &resp.Diagnostics)
	if !ok {
		return
	}

	if err := validateKeysAgainstColumns(columns, partitionKeys, clusteringKeys); err != nil {
		resp.Diagnostics.AddError("Invalid Key Definition", err.Error())
		return
	}

	createStatement := buildCreateTableStatement(plan.Keyspace.ValueString(), plan.TableName.ValueString(), plan.IfNotExists.ValueBool(), columns, partitionKeys, clusteringKeys)
	if !plan.RequiredSystemProfile.IsNull() && !plan.RequiredSystemProfile.IsUnknown() && plan.RequiredSystemProfile.ValueString() != "" {
		settings, exists, err := r.client.GetSystemProfile(plan.RequiredSystemProfile.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to Resolve Required System Profile", err.Error())
			return
		}
		if !exists {
			resp.Diagnostics.AddAttributeError(path.Root("required_system_profile"), "Missing Required System Profile", fmt.Sprintf("System profile %q was not found. A DB admin must create cassandra_system_level_profile %q before this table can be created.", plan.RequiredSystemProfile.ValueString(), plan.RequiredSystemProfile.ValueString()))
			return
		}

		createStatement, err = appendSystemSettingsToCreateStatement(createStatement, settings)
		if err != nil {
			resp.Diagnostics.AddError("Unable to Apply Required System Profile", err.Error())
			return
		}
	}
	if err := r.client.Exec(createStatement); err != nil {
		resp.Diagnostics.AddError("Unable to Create Table", fmt.Sprintf("CQL: %s\nError: %s", createStatement, err))
		return
	}

	for _, index := range saiIndexes {
		stmt := buildCreateSAIIndexStatement(plan.Keyspace.ValueString(), plan.TableName.ValueString(), index)
		if err := r.client.Exec(stmt); err != nil {
			resp.Diagnostics.AddError("Unable to Create SAI Index", fmt.Sprintf("CQL: %s\nError: %s", stmt, err))
			return
		}
	}

	plan.ID = types.StringValue(plan.Keyspace.ValueString() + "." + plan.TableName.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *UserLevelTableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UserLevelTableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	exists, err := r.client.TableExists(state.Keyspace.ValueString(), state.TableName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Table", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
	}
}

func (r *UserLevelTableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan UserLevelTableModel
	var state UserLevelTableModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planColumns, planPartitionKeys, planClusteringKeys, planSAIIndexes, ok := decodeUserLevelValues(ctx, plan, &resp.Diagnostics)
	if !ok {
		return
	}
	stateColumns, statePartitionKeys, stateClusteringKeys, stateSAIIndexes, ok := decodeUserLevelValues(ctx, state, &resp.Diagnostics)
	if !ok {
		return
	}

	if !slices.Equal(planPartitionKeys, statePartitionKeys) {
		resp.Diagnostics.AddAttributeError(path.Root("partition_keys"), "Partition Keys Are Immutable", "Changing partition keys requires a new Cassandra table.")
		return
	}
	if !equalClusteringKeys(planClusteringKeys, stateClusteringKeys) {
		resp.Diagnostics.AddAttributeError(path.Root("clustering_keys"), "Clustering Keys Are Immutable", "Changing clustering keys requires a new Cassandra table.")
		return
	}

	if err := validateKeysAgainstColumns(planColumns, planPartitionKeys, planClusteringKeys); err != nil {
		resp.Diagnostics.AddError("Invalid Key Definition", err.Error())
		return
	}

	if err := applyColumnDiffs(r.client, plan.Keyspace.ValueString(), plan.TableName.ValueString(), stateColumns, planColumns); err != nil {
		resp.Diagnostics.AddError("Unable to Apply Column Migration", err.Error())
		return
	}

	if err := applySAIDiffs(r.client, plan.Keyspace.ValueString(), plan.TableName.ValueString(), stateSAIIndexes, planSAIIndexes); err != nil {
		resp.Diagnostics.AddError("Unable to Apply SAI Migration", err.Error())
		return
	}

	if plan.RequiredSystemProfile.ValueString() != state.RequiredSystemProfile.ValueString() && !plan.RequiredSystemProfile.IsNull() && !plan.RequiredSystemProfile.IsUnknown() {
		settings, exists, err := r.client.GetSystemProfile(plan.RequiredSystemProfile.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to Resolve Required System Profile", err.Error())
			return
		}
		if !exists {
			resp.Diagnostics.AddAttributeError(path.Root("required_system_profile"), "Missing Required System Profile", fmt.Sprintf("System profile %q was not found.", plan.RequiredSystemProfile.ValueString()))
			return
		}

		statement, err := buildAlterTableSettingsStatement(plan.Keyspace.ValueString(), plan.TableName.ValueString(), settings)
		if err != nil {
			resp.Diagnostics.AddError("Unable to Apply Required System Profile", err.Error())
			return
		}
		if err := r.client.Exec(statement); err != nil {
			resp.Diagnostics.AddError("Unable to Apply Required System Profile", fmt.Sprintf("CQL: %s\nError: %s", statement, err))
			return
		}
	}

	plan.ID = types.StringValue(plan.Keyspace.ValueString() + "." + plan.TableName.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserLevelTableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.State.RemoveResource(ctx)
}

func (r *UserLevelTableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func decodeUserLevelValues(ctx context.Context, model UserLevelTableModel, diags *diag.Diagnostics) ([]ColumnModel, []string, []ClusteringKeyModel, []SAIIndexModel, bool) {
	var columns []ColumnModel
	var partitionKeys []string
	var clusteringKeys []ClusteringKeyModel
	var saiIndexes []SAIIndexModel

	diags.Append(model.Columns.ElementsAs(ctx, &columns, false)...)
	diags.Append(model.PartitionKeys.ElementsAs(ctx, &partitionKeys, false)...)

	if !model.ClusteringKeys.IsNull() && !model.ClusteringKeys.IsUnknown() {
		diags.Append(model.ClusteringKeys.ElementsAs(ctx, &clusteringKeys, false)...)
	}
	if !model.SAIIndexes.IsNull() && !model.SAIIndexes.IsUnknown() {
		diags.Append(model.SAIIndexes.ElementsAs(ctx, &saiIndexes, false)...)
	}

	return columns, partitionKeys, clusteringKeys, saiIndexes, !diags.HasError()
}

func validateKeysAgainstColumns(columns []ColumnModel, partitionKeys []string, clusteringKeys []ClusteringKeyModel) error {
	columnSet := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		columnSet[column.Name.ValueString()] = struct{}{}
	}

	for _, key := range partitionKeys {
		if _, ok := columnSet[key]; !ok {
			return fmt.Errorf("partition key %q is not present in columns", key)
		}
	}

	for _, key := range clusteringKeys {
		if _, ok := columnSet[key.Name.ValueString()]; !ok {
			return fmt.Errorf("clustering key %q is not present in columns", key.Name.ValueString())
		}
	}

	return nil
}

func buildCreateTableStatement(keyspace, table string, ifNotExists bool, columns []ColumnModel, partitionKeys []string, clusteringKeys []ClusteringKeyModel) string {
	columnDefs := make([]string, 0, len(columns)+1)
	for _, column := range columns {
		definition := fmt.Sprintf("%s %s", quoteIdentifier(column.Name.ValueString()), column.Type.ValueString())
		if !column.Static.IsNull() && column.Static.ValueBool() {
			definition += " STATIC"
		}
		columnDefs = append(columnDefs, definition)
	}

	columnDefs = append(columnDefs, "PRIMARY KEY "+buildPrimaryKeyClause(partitionKeys, clusteringKeys))

	var builder strings.Builder
	builder.WriteString("CREATE TABLE ")
	if ifNotExists {
		builder.WriteString("IF NOT EXISTS ")
	}
	builder.WriteString(qualifiedTableName(keyspace, table))
	builder.WriteString(" (")
	builder.WriteString(strings.Join(columnDefs, ", "))
	builder.WriteString(")")

	if len(clusteringKeys) > 0 {
		builder.WriteString(" WITH CLUSTERING ORDER BY (")
		orderDefs := make([]string, 0, len(clusteringKeys))
		for _, ck := range clusteringKeys {
			order := "ASC"
			if !ck.Order.IsNull() && ck.Order.ValueString() != "" {
				order = strings.ToUpper(ck.Order.ValueString())
			}
			orderDefs = append(orderDefs, fmt.Sprintf("%s %s", quoteIdentifier(ck.Name.ValueString()), order))
		}
		builder.WriteString(strings.Join(orderDefs, ", "))
		builder.WriteString(")")
	}

	return builder.String()
}

func buildPrimaryKeyClause(partitionKeys []string, clusteringKeys []ClusteringKeyModel) string {
	parts := make([]string, 0, len(clusteringKeys)+1)
	if len(partitionKeys) == 1 {
		parts = append(parts, quoteIdentifier(partitionKeys[0]))
	} else {
		keys := make([]string, 0, len(partitionKeys))
		for _, key := range partitionKeys {
			keys = append(keys, quoteIdentifier(key))
		}
		parts = append(parts, "("+strings.Join(keys, ", ")+")")
	}

	for _, key := range clusteringKeys {
		parts = append(parts, quoteIdentifier(key.Name.ValueString()))
	}

	return "(" + strings.Join(parts, ", ") + ")"
}

func equalClusteringKeys(left, right []ClusteringKeyModel) bool {
	if len(left) != len(right) {
		return false
	}

	for i := range left {
		leftOrder := "ASC"
		if !left[i].Order.IsNull() && left[i].Order.ValueString() != "" {
			leftOrder = strings.ToUpper(left[i].Order.ValueString())
		}
		rightOrder := "ASC"
		if !right[i].Order.IsNull() && right[i].Order.ValueString() != "" {
			rightOrder = strings.ToUpper(right[i].Order.ValueString())
		}
		if left[i].Name.ValueString() != right[i].Name.ValueString() || leftOrder != rightOrder {
			return false
		}
	}

	return true
}

func applyColumnDiffs(client *CassandraClient, keyspace, table string, previous, next []ColumnModel) error {
	previousByName := make(map[string]ColumnModel, len(previous))
	nextByName := make(map[string]ColumnModel, len(next))

	for _, column := range previous {
		previousByName[column.Name.ValueString()] = column
	}
	for _, column := range next {
		nextByName[column.Name.ValueString()] = column
	}

	for name, existing := range previousByName {
		nextColumn, ok := nextByName[name]
		if !ok {
			stmt := fmt.Sprintf("ALTER TABLE %s DROP %s", qualifiedTableName(keyspace, table), quoteIdentifier(name))
			if err := client.Exec(stmt); err != nil {
				return fmt.Errorf("drop column %q failed: %w", name, err)
			}
			continue
		}

		if existing.Type.ValueString() != nextColumn.Type.ValueString() {
			return fmt.Errorf("changing type of column %q from %s to %s is not supported", name, existing.Type.ValueString(), nextColumn.Type.ValueString())
		}
	}

	for name, column := range nextByName {
		if _, ok := previousByName[name]; ok {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s ADD %s %s", qualifiedTableName(keyspace, table), quoteIdentifier(name), column.Type.ValueString())
		if !column.Static.IsNull() && column.Static.ValueBool() {
			stmt += " STATIC"
		}
		if err := client.Exec(stmt); err != nil {
			return fmt.Errorf("add column %q failed: %w", name, err)
		}
	}

	return nil
}

func buildCreateSAIIndexStatement(keyspace, table string, index SAIIndexModel) string {
	stmt := fmt.Sprintf(
		"CREATE CUSTOM INDEX IF NOT EXISTS %s ON %s (%s) USING 'StorageAttachedIndex'",
		quoteIdentifier(index.Name.ValueString()),
		qualifiedTableName(keyspace, table),
		quoteIdentifier(index.Column.ValueString()),
	)

	if index.Options.IsNull() || index.Options.IsUnknown() {
		return stmt
	}

	options := make(map[string]string)
	_ = index.Options.ElementsAs(context.Background(), &options, false)
	if len(options) == 0 {
		return stmt
	}

	parts := make([]string, 0, len(options))
	for key, value := range options {
		parts = append(parts, fmt.Sprintf("%s: %s", quoteStringLiteral(key), quoteStringLiteral(value)))
	}

	return stmt + " WITH OPTIONS = {" + strings.Join(parts, ", ") + "}"
}

func applySAIDiffs(client *CassandraClient, keyspace, table string, previous, next []SAIIndexModel) error {
	previousByName := make(map[string]SAIIndexModel, len(previous))
	nextByName := make(map[string]SAIIndexModel, len(next))

	for _, index := range previous {
		previousByName[index.Name.ValueString()] = index
	}
	for _, index := range next {
		nextByName[index.Name.ValueString()] = index
	}

	for name := range previousByName {
		if _, ok := nextByName[name]; ok {
			continue
		}
		stmt := fmt.Sprintf("DROP INDEX IF EXISTS %s.%s", quoteIdentifier(keyspace), quoteIdentifier(name))
		if err := client.Exec(stmt); err != nil {
			return fmt.Errorf("drop index %q failed: %w", name, err)
		}
	}

	for name, index := range nextByName {
		if previousIndex, ok := previousByName[name]; ok {
			if previousIndex.Column.ValueString() != index.Column.ValueString() {
				return fmt.Errorf("changing indexed column for SAI %q is not supported in-place", name)
			}
			continue
		}
		stmt := buildCreateSAIIndexStatement(keyspace, table, index)
		if err := client.Exec(stmt); err != nil {
			return fmt.Errorf("create index %q failed: %w", name, err)
		}
	}

	return nil
}
