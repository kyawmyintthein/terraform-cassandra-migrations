package provider

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	replicationClassSimpleStrategy          = "SimpleStrategy"
	replicationClassNetworkTopologyStrategy = "NetworkTopologyStrategy"
)

type SystemKeyspacePolicy struct {
	ReplicationClass         string            `json:"replication_class"`
	ReplicationFactor        *int64            `json:"replication_factor,omitempty"`
	RegionReplicationFactors map[string]string `json:"region_replication_factors,omitempty"`
	DurableWrites            *bool             `json:"durable_writes,omitempty"`
}

func decodeRegions(ctx context.Context, value types.List) ([]string, error) {
	regions := []string{}
	if value.IsNull() || value.IsUnknown() {
		return regions, nil
	}

	diags := value.ElementsAs(ctx, &regions, false)
	if diags.HasError() {
		return nil, fmt.Errorf("regions must be a list of strings")
	}
	return normalizeRegions(regions)
}

func normalizeRegions(regions []string) ([]string, error) {
	normalized := make([]string, 0, len(regions))
	seen := make(map[string]struct{}, len(regions))

	for _, region := range regions {
		trimmed := strings.TrimSpace(region)
		if trimmed == "" {
			return nil, fmt.Errorf("region names must not be empty")
		}
		if _, ok := seen[trimmed]; ok {
			return nil, fmt.Errorf("region %q was listed more than once", trimmed)
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	sort.Strings(normalized)
	return normalized, nil
}

func validateSystemKeyspacePolicy(policy SystemKeyspacePolicy) error {
	switch policy.ReplicationClass {
	case replicationClassSimpleStrategy:
		if policy.ReplicationFactor == nil || *policy.ReplicationFactor < 1 {
			return fmt.Errorf("replication_factor must be at least 1 when replication_class is %q", replicationClassSimpleStrategy)
		}
		if len(policy.RegionReplicationFactors) > 0 {
			return fmt.Errorf("region_replication_factors is only valid when replication_class is %q", replicationClassNetworkTopologyStrategy)
		}
	case replicationClassNetworkTopologyStrategy:
		if policy.ReplicationFactor != nil {
			return fmt.Errorf("replication_factor is not valid when replication_class is %q; use region_replication_factors instead", replicationClassNetworkTopologyStrategy)
		}
		if len(policy.RegionReplicationFactors) == 0 {
			return fmt.Errorf("region_replication_factors must include at least one region when replication_class is %q", replicationClassNetworkTopologyStrategy)
		}
		for region, factor := range policy.RegionReplicationFactors {
			if strings.TrimSpace(region) == "" {
				return fmt.Errorf("region_replication_factors must not contain an empty region name")
			}
			value, err := strconv.ParseInt(strings.TrimSpace(factor), 10, 64)
			if err != nil || value < 1 {
				return fmt.Errorf("region_replication_factors[%q] must be an integer string of at least 1", region)
			}
		}
	default:
		return fmt.Errorf("unsupported replication_class %q", policy.ReplicationClass)
	}

	return nil
}

func buildReplicationFromPolicy(policy SystemKeyspacePolicy, selectedRegions []string) (map[string]string, error) {
	replication := map[string]string{
		"class": policy.ReplicationClass,
	}

	switch policy.ReplicationClass {
	case replicationClassSimpleStrategy:
		if len(selectedRegions) > 0 {
			return nil, fmt.Errorf("regions cannot be set when the policy uses %q", replicationClassSimpleStrategy)
		}
		replication["replication_factor"] = strconv.FormatInt(*policy.ReplicationFactor, 10)
	case replicationClassNetworkTopologyStrategy:
		if len(selectedRegions) == 0 {
			return nil, fmt.Errorf("at least one region must be selected when the policy uses %q", replicationClassNetworkTopologyStrategy)
		}

		for _, region := range selectedRegions {
			factor, ok := policy.RegionReplicationFactors[region]
			if !ok {
				return nil, fmt.Errorf("region %q is not allowed by the selected keyspace policy", region)
			}
			replication[region] = factor
		}
	default:
		return nil, fmt.Errorf("unsupported replication_class %q", policy.ReplicationClass)
	}

	return replication, nil
}

func buildCreateKeyspaceStatement(keyspace string, ifNotExists bool, replication map[string]string, durableWrites bool) string {
	var builder strings.Builder
	builder.WriteString("CREATE KEYSPACE ")
	if ifNotExists {
		builder.WriteString("IF NOT EXISTS ")
	}
	builder.WriteString(quoteIdentifier(keyspace))
	builder.WriteString(" WITH replication = ")
	builder.WriteString(buildReplicationLiteral(replication))
	builder.WriteString(" AND durable_writes = ")
	builder.WriteString(strconv.FormatBool(durableWrites))
	return builder.String()
}

func buildAlterKeyspaceStatement(keyspace string, replication map[string]string, durableWrites bool) string {
	return fmt.Sprintf(
		"ALTER KEYSPACE %s WITH replication = %s AND durable_writes = %s",
		quoteIdentifier(keyspace),
		buildReplicationLiteral(replication),
		strconv.FormatBool(durableWrites),
	)
}

func validateNoRegionRemoval(previous, next []string) error {
	for _, existing := range previous {
		if !slices.Contains(next, existing) {
			return fmt.Errorf("removing region %q from an existing keyspace is not supported in-place; coordinate that migration manually", existing)
		}
	}
	return nil
}

func missingSystemKeyspacePolicyDiagnostic(policyName string) (path.Path, string, string) {
	return path.Root("required_system_keyspace_policy"),
		"Missing Required System Keyspace Policy",
		fmt.Sprintf("System keyspace policy %q was not found. A platform team must create cassandra_system_level_keyspace_policy %q before this keyspace can be managed.", policyName, policyName)
}
