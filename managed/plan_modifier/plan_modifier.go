package planmodifier

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	fwplanmodifier "github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// ImmutableFieldModifier is a plan modifier that enforces immutability of an attribute.
// This should be used with attribute's that can't be changed after resource creation and you
// want to show error to user during `tf plan` rather than `tf apply`.
//
// It should NOT be used for attribute's having "Computed: true" as they are already Read-Only.
//
// For nested attribute types, applying this plan modifier at root attribute is enough.
type ImmutableFieldModifier struct{}

var (
	_ fwplanmodifier.String = ImmutableFieldModifier{}
	_ fwplanmodifier.Object = ImmutableFieldModifier{}
)

func (m ImmutableFieldModifier) modify(p path.Path, config, state attr.Value, diags *diag.Diagnostics) {
	if state.IsNull() {
		return
	}

	if !config.IsNull() && !config.Equal(state) {
		diags.AddError(
			"Immutable Field Error",
			fmt.Sprintf(
				"Field '%s' is immutable and cannot be updated. Please destroy and recreate the resource if changes are needed.",
				p.String(),
			),
		)
	}
}

func (m ImmutableFieldModifier) PlanModifyString(_ context.Context, req fwplanmodifier.StringRequest, resp *fwplanmodifier.StringResponse) {
	m.modify(req.Path, req.ConfigValue, req.StateValue, &resp.Diagnostics)
}

func (m ImmutableFieldModifier) PlanModifyObject(_ context.Context, req fwplanmodifier.ObjectRequest, resp *fwplanmodifier.ObjectResponse) {
	m.modify(req.Path, req.ConfigValue, req.StateValue, &resp.Diagnostics)
}

func (m ImmutableFieldModifier) Description(ctx context.Context) string {
	return "Errors if the field is changed after resource creation"
}

func (m ImmutableFieldModifier) MarkdownDescription(ctx context.Context) string {
	return "Errors if the field is changed after resource creation"
}
