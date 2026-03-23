package provider

import (
	"reflect"
	"testing"
)

func TestBuildReplicationFromPolicyNetworkTopologyStrategy(t *testing.T) {
	replication, err := buildReplicationFromPolicy(SystemKeyspacePolicy{
		ReplicationClass: replicationClassNetworkTopologyStrategy,
		RegionReplicationFactors: map[string]string{
			"ap-southeast-2": "3",
			"us-east-1":      "2",
		},
	}, []string{"us-east-1", "ap-southeast-2"})
	if err != nil {
		t.Fatalf("buildReplicationFromPolicy returned error: %v", err)
	}

	expected := map[string]string{
		"class":          replicationClassNetworkTopologyStrategy,
		"ap-southeast-2": "3",
		"us-east-1":      "2",
	}
	if !reflect.DeepEqual(replication, expected) {
		t.Fatalf("unexpected replication map: got %#v want %#v", replication, expected)
	}
}

func TestBuildReplicationFromPolicyRejectsDisallowedRegion(t *testing.T) {
	_, err := buildReplicationFromPolicy(SystemKeyspacePolicy{
		ReplicationClass: replicationClassNetworkTopologyStrategy,
		RegionReplicationFactors: map[string]string{
			"ap-southeast-2": "3",
		},
	}, []string{"us-east-1"})
	if err == nil {
		t.Fatal("expected an error for disallowed region")
	}
}

func TestValidateSystemKeyspacePolicyRejectsUniformReplicationFactorForNetworkTopology(t *testing.T) {
	replicationFactor := int64(3)
	err := validateSystemKeyspacePolicy(SystemKeyspacePolicy{
		ReplicationClass:  replicationClassNetworkTopologyStrategy,
		ReplicationFactor: &replicationFactor,
	})
	if err == nil {
		t.Fatal("expected an error when NetworkTopologyStrategy uses replication_factor")
	}
}

func TestValidateNoRegionRemoval(t *testing.T) {
	err := validateNoRegionRemoval([]string{"ap-southeast-2", "us-east-1"}, []string{"ap-southeast-2"})
	if err == nil {
		t.Fatal("expected an error when removing a region")
	}
}
