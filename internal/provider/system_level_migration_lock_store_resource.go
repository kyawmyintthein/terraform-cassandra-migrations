package provider

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = (*SystemLevelMigrationLockStoreResource)(nil)
var _ resource.ResourceWithConfigure = (*SystemLevelMigrationLockStoreResource)(nil)
var _ resource.ResourceWithImportState = (*SystemLevelMigrationLockStoreResource)(nil)

type SystemLevelMigrationLockStoreResource struct {
	client *CassandraClient
}

type SystemLevelMigrationLockStoreModel struct {
	ID          types.String `tfsdk:"id"`
	Keyspace    types.String `tfsdk:"keyspace"`
	TableName   types.String `tfsdk:"table_name"`
	Replication types.Map    `tfsdk:"replication"`
}

func NewSystemLevelMigrationLockStoreResource() resource.Resource {
	return &SystemLevelMigrationLockStoreResource{}
}

func (r *SystemLevelMigrationLockStoreResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_system_level_migration_lock_store"
}

func (r *SystemLevelMigrationLockStoreResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Defines the platform-managed Cassandra keyspace and table used for schema migration locks.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"keyspace": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Keyspace that will hold the migration lock table. Must match the provider migration_lock_keyspace setting used by user-level applies.",
			},
			"table_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Table that will hold per-table migration lock rows. Must match the provider migration_lock_table setting used by user-level applies.",
			},
			"replication": schema.MapAttribute{
				ElementType:         types.StringType,
				Required:            true,
				MarkdownDescription: "Cassandra keyspace replication map, for example { class = \"NetworkTopologyStrategy\", dc1 = \"3\" }.",
			},
		},
	}
}

func (r *SystemLevelMigrationLockStoreResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*CassandraClient)
}

func (r *SystemLevelMigrationLockStoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SystemLevelMigrationLockStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.applyLockStore(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SystemLevelMigrationLockStoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SystemLevelMigrationLockStoreModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	exists, err := r.client.TableExists(state.Keyspace.ValueString(), state.TableName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Migration Lock Store", err.Error())
		return
	}
	if !exists {
		resp.State.RemoveResource(ctx)
	}
}

func (r *SystemLevelMigrationLockStoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SystemLevelMigrationLockStoreModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.applyLockStore(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SystemLevelMigrationLockStoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.State.RemoveResource(ctx)
}

func (r *SystemLevelMigrationLockStoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *SystemLevelMigrationLockStoreResource) applyLockStore(ctx context.Context, plan *SystemLevelMigrationLockStoreModel, diags *diag.Diagnostics) {
	if err := validateLockStoreMatchesProvider(r.client, plan); err != nil {
		diags.AddError("Migration Lock Store Does Not Match Provider Configuration", err.Error())
		return
	}

	replication, err := decodeStringMap(ctx, plan.Replication)
	if err != nil {
		diags.AddError("Invalid Replication Definition", err.Error())
		return
	}
	if err := validateReplicationMap(replication); err != nil {
		diags.AddAttributeError(path.Root("replication"), "Invalid Replication Definition", err.Error())
		return
	}

	keyspaceStmt := fmt.Sprintf(
		"CREATE KEYSPACE IF NOT EXISTS %s WITH replication = %s",
		quoteIdentifier(plan.Keyspace.ValueString()),
		buildReplicationLiteral(replication),
	)
	if err := r.client.ExecSchemaMutation(ctx, keyspaceStmt); err != nil {
		diags.AddError("Unable to Create Migration Lock Keyspace", fmt.Sprintf("CQL: %s\nError: %s", keyspaceStmt, err))
		return
	}

	tableStmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s.%s (resource_id text PRIMARY KEY, lock_token text, owner text, operation text, started_at timestamp, last_heartbeat timestamp, lease_expires_at timestamp)",
		quoteIdentifier(plan.Keyspace.ValueString()),
		quoteIdentifier(plan.TableName.ValueString()),
	)
	if err := r.client.ExecSchemaMutation(ctx, tableStmt); err != nil {
		diags.AddError("Unable to Create Migration Lock Table", fmt.Sprintf("CQL: %s\nError: %s", tableStmt, err))
		return
	}

	plan.ID = types.StringValue(plan.Keyspace.ValueString() + "." + plan.TableName.ValueString())
}

func validateLockStoreMatchesProvider(client *CassandraClient, plan *SystemLevelMigrationLockStoreModel) error {
	if client.migrationLockKeyspace != plan.Keyspace.ValueString() {
		return fmt.Errorf("resource keyspace %q must match provider migration_lock_keyspace %q", plan.Keyspace.ValueString(), client.migrationLockKeyspace)
	}
	if client.migrationLockTable != plan.TableName.ValueString() {
		return fmt.Errorf("resource table_name %q must match provider migration_lock_table %q", plan.TableName.ValueString(), client.migrationLockTable)
	}
	return nil
}

func decodeStringMap(ctx context.Context, value types.Map) (map[string]string, error) {
	decoded := map[string]string{}
	if value.IsNull() || value.IsUnknown() {
		return decoded, nil
	}

	diags := value.ElementsAs(ctx, &decoded, false)
	if diags.HasError() {
		return nil, fmt.Errorf("replication must be a string map")
	}
	return decoded, nil
}

func validateReplicationMap(replication map[string]string) error {
	if len(replication) == 0 {
		return fmt.Errorf("replication must include at least a class entry")
	}
	class := strings.TrimSpace(replication["class"])
	if class == "" {
		return fmt.Errorf("replication must include a non-empty class entry")
	}

	switch class {
	case replicationClassSimpleStrategy:
		factor, err := strconv.ParseInt(strings.TrimSpace(replication["replication_factor"]), 10, 64)
		if err != nil || factor < 1 {
			return fmt.Errorf("replication_factor must be an integer string of at least 1 when class is %q", replicationClassSimpleStrategy)
		}
	case replicationClassNetworkTopologyStrategy:
		if len(replication) < 2 {
			return fmt.Errorf("replication must include at least one datacenter or region entry when class is %q", replicationClassNetworkTopologyStrategy)
		}
		for key, value := range replication {
			if key == "class" {
				continue
			}
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("replication must not contain an empty datacenter or region name")
			}

			factor, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil || factor < 1 {
				return fmt.Errorf("replication[%q] must be an integer string of at least 1", key)
			}
		}
	default:
		if len(replication) == 1 {
			return fmt.Errorf("replication for class %q must include at least one strategy-specific option", class)
		}
	}

	return nil
}

func buildReplicationLiteral(replication map[string]string) string {
	keys := make([]string, 0, len(replication))
	for key := range replication {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", quoteStringLiteral(key), quoteStringLiteral(replication[key])))
	}

	return "{" + strings.Join(parts, ", ") + "}"
}
