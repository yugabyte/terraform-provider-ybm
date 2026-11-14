package planmodifier

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func knownFT(v string) types.String { return types.String{Value: v} }

var (
	nullFT    = types.String{Null: true}
	unknownFT = types.String{Unknown: true}
)

func TestFaultToleranceChangeRequiresReplace(t *testing.T) {
	tests := []struct {
		name    string
		prior   attr.Value
		planned attr.Value
		want    bool
	}{
		{"NONE to ZONE", knownFT("NONE"), knownFT("ZONE"), true},
		{"NONE to NODE", knownFT("NONE"), knownFT("NODE"), true},
		{"NONE to REGION", knownFT("NONE"), knownFT("REGION"), true},
		{"NONE to NONE", knownFT("NONE"), knownFT("NONE"), false},
		{"ZONE to ZONE", knownFT("ZONE"), knownFT("ZONE"), false},
		// Only NONE -> other is confirmed as refused by the API.
		{"ZONE to REGION", knownFT("ZONE"), knownFT("REGION"), false},
		{"ZONE to NONE", knownFT("ZONE"), knownFT("NONE"), false},
		{"null prior (create)", nullFT, knownFT("ZONE"), false},
		{"unknown prior", unknownFT, knownFT("ZONE"), false},
		{"null planned", knownFT("NONE"), nullFT, false},
		{"unknown planned", knownFT("NONE"), unknownFT, false},
		{"nil prior", nil, knownFT("ZONE"), false},
		{"pointer values", &types.String{Value: "NONE"}, &types.String{Value: "ZONE"}, true},
		{"non-string value", types.Bool{Value: true}, knownFT("ZONE"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FaultToleranceChangeRequiresReplace(tt.prior, tt.planned); got != tt.want {
				t.Errorf("FaultToleranceChangeRequiresReplace(%v, %v) = %v, want %v", tt.prior, tt.planned, got, tt.want)
			}
		})
	}
}

// testClusterModel mirrors the minimal slice of the ybm_cluster schema needed
// to exercise the plan modifier through the framework.
type testClusterModel struct {
	FaultTolerance types.String `tfsdk:"fault_tolerance"`
}

func testClusterSchema() tfsdk.Schema {
	return tfsdk.Schema{
		Attributes: map[string]tfsdk.Attribute{
			"fault_tolerance": {
				Type:     types.StringType,
				Optional: true,
				Computed: true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
					FaultToleranceRequiresReplace(),
				},
			},
		},
	}
}

// TestFaultToleranceRequiresReplaceModifier runs the modifier as the framework
// would, so the create/destroy/null-config short-circuits in
// tfsdk.RequiresReplaceIf are covered too.
func TestFaultToleranceRequiresReplaceModifier(t *testing.T) {
	ctx := context.Background()
	schema := testClusterSchema()

	tests := []struct {
		name        string
		state       *types.String // nil means no prior state (create)
		config      types.String
		plan        *types.String // nil means null plan (destroy)
		wantReplace bool
	}{
		{name: "NONE to ZONE forces replacement", state: ptr(knownFT("NONE")), config: knownFT("ZONE"), plan: ptr(knownFT("ZONE")), wantReplace: true},
		{name: "ZONE to ZONE is a no-op", state: ptr(knownFT("ZONE")), config: knownFT("ZONE"), plan: ptr(knownFT("ZONE")), wantReplace: false},
		{name: "NONE to NONE is a no-op", state: ptr(knownFT("NONE")), config: knownFT("NONE"), plan: ptr(knownFT("NONE")), wantReplace: false},
		{name: "ZONE to REGION stays in place", state: ptr(knownFT("ZONE")), config: knownFT("REGION"), plan: ptr(knownFT("REGION")), wantReplace: false},
		{name: "create with no prior state", state: nil, config: knownFT("ZONE"), plan: ptr(knownFT("ZONE")), wantReplace: false},
		{name: "create with null prior value", state: ptr(nullFT), config: knownFT("ZONE"), plan: ptr(knownFT("ZONE")), wantReplace: false},
		{name: "attribute removed from config", state: ptr(knownFT("NONE")), config: nullFT, plan: ptr(knownFT("NONE")), wantReplace: false},
		{name: "config unknown at plan time", state: ptr(knownFT("NONE")), config: unknownFT, plan: ptr(unknownFT), wantReplace: false},
		{name: "destroy", state: ptr(knownFT("NONE")), config: nullFT, plan: nil, wantReplace: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := tfsdk.State{Schema: schema}
			stateValue := attr.Value(nullFT)
			if tt.state != nil {
				if diags := state.Set(ctx, &testClusterModel{FaultTolerance: *tt.state}); diags.HasError() {
					t.Fatalf("setting state: %v", diags)
				}
				stateValue = *tt.state
			}

			plan := tfsdk.Plan{Schema: schema}
			planValue := attr.Value(nullFT)
			if tt.plan != nil {
				if diags := plan.Set(ctx, &testClusterModel{FaultTolerance: *tt.plan}); diags.HasError() {
					t.Fatalf("setting plan: %v", diags)
				}
				planValue = *tt.plan
			}

			req := tfsdk.ModifyAttributePlanRequest{
				AttributePath:   path.Root("fault_tolerance"),
				Config:          tfsdk.Config{Schema: schema},
				State:           state,
				Plan:            plan,
				AttributeConfig: tt.config,
				AttributeState:  stateValue,
				AttributePlan:   planValue,
			}
			resp := &tfsdk.ModifyAttributePlanResponse{AttributePlan: planValue}

			FaultToleranceRequiresReplace().Modify(ctx, req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
			if resp.RequiresReplace != tt.wantReplace {
				t.Errorf("RequiresReplace = %v, want %v", resp.RequiresReplace, tt.wantReplace)
			}
		})
	}
}

func ptr(s types.String) *types.String { return &s }
