/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// clusterRegionInfoValue builds a cluster_region_info list holding one region whose is_default
// carries the given value, with every other region attribute null.
func clusterRegionInfoValue(t *testing.T, regionType tftypes.Type, isDefault tftypes.Value) tftypes.Value {
	t.Helper()
	object, ok := regionType.(tftypes.Object)
	if !ok {
		t.Fatalf("cluster_region_info element is %T, want tftypes.Object", regionType)
	}
	attrs := map[string]tftypes.Value{}
	for name, attrType := range object.AttributeTypes {
		attrs[name] = tftypes.NewValue(attrType, nil)
	}
	attrs["region"] = tftypes.NewValue(tftypes.String, "us-west2")
	attrs["num_nodes"] = tftypes.NewValue(tftypes.Number, 3)
	attrs["is_default"] = isDefault
	return tftypes.NewValue(tftypes.List{ElementType: regionType}, []tftypes.Value{tftypes.NewValue(regionType, attrs)})
}

// clusterValue builds a whole ybm_cluster value that is null everywhere but cluster_region_info.
func clusterValue(t *testing.T, schemaType tftypes.Type, regions tftypes.Value) tftypes.Value {
	t.Helper()
	object := schemaType.(tftypes.Object)
	attrs := map[string]tftypes.Value{}
	for name, attrType := range object.AttributeTypes {
		attrs[name] = tftypes.NewValue(attrType, nil)
	}
	attrs["cluster_region_info"] = regions
	return tftypes.NewValue(schemaType, attrs)
}

func runModifyPlan(t *testing.T, planDefault, configDefault tftypes.Value, hasState bool) tfsdk.ModifyResourcePlanResponse {
	t.Helper()
	ctx := context.Background()
	schema, diags := resourceClusterType{}.GetSchema(ctx)
	if diags.HasError() {
		t.Fatalf("GetSchema: %v", diags)
	}
	schemaType := schema.TerraformType(ctx)
	regionType := schemaType.(tftypes.Object).AttributeTypes["cluster_region_info"].(tftypes.List).ElementType

	plan := tfsdk.Plan{Schema: schema, Raw: clusterValue(t, schemaType, clusterRegionInfoValue(t, regionType, planDefault))}
	config := tfsdk.Config{Schema: schema, Raw: clusterValue(t, schemaType, clusterRegionInfoValue(t, regionType, configDefault))}
	state := tfsdk.State{Schema: schema, Raw: tftypes.NewValue(schemaType, nil)}
	if hasState {
		state.Raw = clusterValue(t, schemaType, clusterRegionInfoValue(t, regionType, planDefault))
	}

	req := tfsdk.ModifyResourcePlanRequest{Config: config, Plan: plan, State: state}
	resp := tfsdk.ModifyResourcePlanResponse{Plan: plan}
	resourceCluster{}.ModifyPlan(ctx, req, &resp)
	return resp
}

func plannedIsDefault(t *testing.T, resp tfsdk.ModifyResourcePlanResponse) types.Bool {
	t.Helper()
	var planned types.Bool
	diags := resp.Plan.GetAttribute(context.Background(),
		path.Root("cluster_region_info").AtListIndex(0).AtName("is_default"), &planned)
	if diags.HasError() {
		t.Fatalf("read planned is_default: %v", diags)
	}
	return planned
}

// Shrinking a multi-region cluster to one region leaves is_default=false in the plan from prior
// state; the apply forces true, so the plan value has to become unknown.
func TestModifyPlanSingleRegionDefaultFalseBecomesUnknown(t *testing.T) {
	resp := runModifyPlan(t,
		tftypes.NewValue(tftypes.Bool, false), // plan, carried over from state
		tftypes.NewValue(tftypes.Bool, nil),   // config leaves is_default unset
		true)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if planned := plannedIsDefault(t, resp); !planned.Unknown {
		t.Fatalf("planned is_default = %+v, want unknown", planned)
	}
}

// An is_default the config really did set to false can never hold, and is reported at plan time.
func TestModifyPlanSingleRegionExplicitFalseIsAnError(t *testing.T) {
	resp := runModifyPlan(t,
		tftypes.NewValue(tftypes.Bool, false),
		tftypes.NewValue(tftypes.Bool, false),
		true)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error for an explicit is_default = false")
	}
}

// A config that sets is_default = true matches what the apply returns and is left alone.
func TestModifyPlanSingleRegionTrueIsLeftAlone(t *testing.T) {
	resp := runModifyPlan(t,
		tftypes.NewValue(tftypes.Bool, true),
		tftypes.NewValue(tftypes.Bool, true),
		true)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	planned := plannedIsDefault(t, resp)
	if planned.Unknown || !planned.Value {
		t.Fatalf("planned is_default = %+v, want true", planned)
	}
}

// On create the plan value is already unknown and must stay that way.
func TestModifyPlanSingleRegionCreateKeepsUnknown(t *testing.T) {
	resp := runModifyPlan(t,
		tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue),
		tftypes.NewValue(tftypes.Bool, nil),
		false)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if planned := plannedIsDefault(t, resp); !planned.Unknown {
		t.Fatalf("planned is_default = %+v, want unknown", planned)
	}
}
