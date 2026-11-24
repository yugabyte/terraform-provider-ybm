/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// createFixture returns a plan for a new resource of res as Terraform sends it (the given
// configured values known, everything else unknown) and an empty Create response.
func createFixture(t *testing.T, res resource.Resource, known map[string]string) (tfsdk.Plan, *resource.CreateResponse) {
	t.Helper()
	ctx := context.Background()
	schemaResp := &resource.SchemaResponse{}
	res.Schema(ctx, resource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", schemaResp.Diagnostics)
	}
	s := schemaResp.Schema
	objType := s.Type().TerraformType(ctx).(tftypes.Object)
	vals := map[string]tftypes.Value{}
	for name, typ := range objType.AttributeTypes {
		vals[name] = tftypes.NewValue(typ, tftypes.UnknownValue)
	}
	for name, v := range known {
		vals[name] = tftypes.NewValue(tftypes.String, v)
	}
	plan := tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(objType, vals)}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, nil)}}
	return plan, resp
}

func stringAttrs(m map[string]string) map[string]attr.Value {
	out := map[string]attr.Value{}
	for k, v := range m {
		out[k] = types.StringValue(v)
	}
	return out
}

// A resource that the API has created must be recorded in state, with the IDs its Read needs and
// without any unknown values, so that a failure in a later Create step does not orphan it. The
// attribute names here are the ones each Create passes to recordCreatedResource.
func TestRecordCreatedResource(t *testing.T) {
	cases := []struct {
		name  string
		res   resource.Resource
		known map[string]string // a configured value, as the plan has it
		ids   map[string]string
	}{
		{"cluster", &resourceCluster{}, map[string]string{"cluster_name": "c1"},
			map[string]string{"account_id": "acc", "project_id": "proj", "cluster_id": "id-1"}},
		{"read replicas", &resourceReadReplicas{}, nil,
			map[string]string{"account_id": "acc", "project_id": "proj", "primary_cluster_id": "id-1"}},
		{"vpc", &resourceVPC{}, map[string]string{"name": "v1"},
			map[string]string{"account_id": "acc", "project_id": "proj", "vpc_id": "id-1"}},
		{"vpc peering", &resourceVPCPeering{}, map[string]string{"name": "p1"},
			map[string]string{"account_id": "acc", "project_id": "proj", "vpc_peering_id": "id-1"}},
		{"private endpoint", &resourcePrivateEndpoint{}, map[string]string{"region": "us-east-1"},
			map[string]string{"account_id": "acc", "project_id": "proj", "endpoint_id": "id-1", "cluster_id": "c", "cluster_region_info_id": "r"}},
		{"dr config", &resourceDrConfig{}, map[string]string{"name": "dr1"},
			map[string]string{"account_id": "acc", "project_id": "proj", "dr_config_id": "id-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			plan, resp := createFixture(t, tc.res, tc.known)

			if !recordCreatedResource(ctx, plan, resp, tc.name, "id-1", stringAttrs(tc.ids)) {
				t.Fatalf("recordCreatedResource: %v", resp.Diagnostics)
			}
			if !resp.State.Raw.IsFullyKnown() {
				t.Fatal("state still has unknown values")
			}
			want := map[string]string{}
			for k, v := range tc.known {
				want[k] = v
			}
			for k, v := range tc.ids {
				want[k] = v
			}
			for name, w := range want {
				var got types.String
				resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root(name), &got)...)
				if got.ValueString() != w {
					t.Errorf("%s = %q, want %q", name, got.ValueString(), w)
				}
			}
			if resp.Diagnostics.HasError() {
				t.Fatalf("reading state: %v", resp.Diagnostics)
			}
		})
	}
}

// The cluster's restore_backup_id must not be recorded before the restore has run: the API does
// not return it, so a kept (untainted) cluster would otherwise never retry a failed restore.
func TestRecordCreatedClusterLeavesRestoreBackupIDNull(t *testing.T) {
	ctx := context.Background()
	plan, resp := createFixture(t, &resourceCluster{}, map[string]string{"restore_backup_id": "backup-1"})
	attrs := stringAttrs(map[string]string{"account_id": "acc", "project_id": "proj", "cluster_id": "id-1"})
	attrs["restore_backup_id"] = types.StringNull()

	if !recordCreatedResource(ctx, plan, resp, "Cluster", "id-1", attrs) {
		t.Fatalf("recordCreatedResource: %v", resp.Diagnostics)
	}
	var got types.String
	resp.State.GetAttribute(ctx, path.Root("restore_backup_id"), &got)
	if !got.IsNull() {
		t.Errorf("restore_backup_id = %q, want null", got.ValueString())
	}
}

// An error that an earlier step of Create already added must not make the recording fail.
func TestRecordCreatedResourceIgnoresEarlierErrors(t *testing.T) {
	plan, resp := createFixture(t, &resourceVPC{}, map[string]string{"name": "v1"})
	resp.Diagnostics.AddError("earlier step", "failed")

	if !recordCreatedResource(context.Background(), plan, resp, "VPC", "id-1", stringAttrs(map[string]string{"vpc_id": "id-1"})) {
		t.Fatalf("recordCreatedResource returned false because of an earlier error: %v", resp.Diagnostics)
	}
	if resp.Diagnostics.ErrorsCount() != 1 {
		t.Errorf("errors = %v, want only the earlier one", resp.Diagnostics.Errors())
	}
}

// If recording fails, the state is left untouched and only a warning is added, so that Create can
// carry on and still set the final state.
func TestRecordCreatedResourceFailureOnlyWarns(t *testing.T) {
	plan, resp := createFixture(t, &resourceVPC{}, map[string]string{"name": "v1"})
	before := resp.State.Raw.Copy()

	if recordCreatedResource(context.Background(), plan, resp, "VPC", "id-1", stringAttrs(map[string]string{"vpc_id": "id-1", "no_such_attribute": "x"})) {
		t.Fatal("recordCreatedResource reported success for an unknown attribute")
	}
	if !resp.State.Raw.Equal(before) {
		t.Error("state was changed although recording failed")
	}
	if resp.Diagnostics.ErrorsCount() != 0 || resp.Diagnostics.WarningsCount() != 1 {
		t.Errorf("diagnostics = %v, want exactly one warning", resp.Diagnostics)
	}
}

func TestWarnIfCreateIncompleteOnlyOnError(t *testing.T) {
	resp := &resource.CreateResponse{}
	warnIfCreateIncomplete(resp, "VPC", "id-1")
	if len(resp.Diagnostics) != 0 {
		t.Fatalf("warned without an error: %v", resp.Diagnostics)
	}
	resp.Diagnostics.AddError("wait failed", "timed out")
	warnIfCreateIncomplete(resp, "VPC", "id-1")
	if resp.Diagnostics.WarningsCount() != 1 || !strings.Contains(resp.Diagnostics.Warnings()[0].Detail(), "terraform untaint") {
		t.Fatalf("expected one untaint warning, got %v", resp.Diagnostics)
	}
}

func TestClusterHasAllowLists(t *testing.T) {
	attached := []string{"al-2", "al-1"}
	client := newTestYcpClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/clusters/broken/") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var data []map[string]interface{}
		for _, id := range attached {
			data = append(data, map[string]interface{}{"info": map[string]interface{}{"id": id}})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	})
	ctx := context.Background()

	cases := []struct {
		name    string
		cluster string
		desired []string
		want    bool
	}{
		{"same lists in another order", "c", []string{"al-1", "al-2"}, true},
		{"one missing", "c", []string{"al-1"}, false},
		{"one extra", "c", []string{"al-1", "al-2", "al-3"}, false},
		{"list call fails", "broken", []string{"al-1", "al-2"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clusterHasAllowLists(ctx, client, "acc", "proj", tc.cluster, tc.desired); got != tc.want {
				t.Errorf("clusterHasAllowLists = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsClusterCreateFailedState(t *testing.T) {
	for state, want := range map[string]bool{"Create Failed": true, "CREATE_FAILED": true, "Active": false, "Queued": false, "": false} {
		if got := isClusterCreateFailedState(state); got != want {
			t.Errorf("isClusterCreateFailedState(%q) = %v, want %v", state, got, want)
		}
	}
}
