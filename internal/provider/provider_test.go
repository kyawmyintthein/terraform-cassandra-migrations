package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestReadSystemMetadataReplicationConfigUsesDefault(t *testing.T) {
	replication, err := readSystemMetadataReplicationConfig(context.Background(), types.MapNull(types.StringType))
	if err != nil {
		t.Fatalf("readSystemMetadataReplicationConfig returned error: %v", err)
	}

	expected := map[string]string{
		"class":              replicationClassSimpleStrategy,
		"replication_factor": "1",
	}
	if !reflect.DeepEqual(replication, expected) {
		t.Fatalf("unexpected default replication: got %#v want %#v", replication, expected)
	}
}

func TestReadSystemMetadataReplicationConfigUsesCustomReplication(t *testing.T) {
	replication, err := readSystemMetadataReplicationConfig(context.Background(), types.MapValueMust(
		types.StringType,
		map[string]attr.Value{
			"class": types.StringValue(replicationClassNetworkTopologyStrategy),
			"dc1":   types.StringValue("3"),
		},
	))
	if err != nil {
		t.Fatalf("readSystemMetadataReplicationConfig returned error: %v", err)
	}

	expected := map[string]string{
		"class": replicationClassNetworkTopologyStrategy,
		"dc1":   "3",
	}
	if !reflect.DeepEqual(replication, expected) {
		t.Fatalf("unexpected custom replication: got %#v want %#v", replication, expected)
	}
}

func TestReadSystemMetadataReplicationConfigRejectsMissingClass(t *testing.T) {
	_, err := readSystemMetadataReplicationConfig(context.Background(), types.MapValueMust(
		types.StringType,
		map[string]attr.Value{
			"replication_factor": types.StringValue("2"),
		},
	))
	if err == nil {
		t.Fatal("expected an error when replication class is missing")
	}
}

func TestValidateReplicationMapRejectsSimpleStrategyWithoutReplicationFactor(t *testing.T) {
	err := validateReplicationMap(map[string]string{
		"class": replicationClassSimpleStrategy,
	})
	if err == nil {
		t.Fatal("expected an error when SimpleStrategy replication_factor is missing")
	}
}
