package planmodifier

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

// ImmutableFieldModifier is a plan modifier that enforces immutability of an attribute.
// This should be used with attribute's that can't be changed after resource creation and you
// want to show error to user during `tf plan` rather than `tf apply`.
//
// It should NOT be used for attribute's having "Computed: true" as they are already Read-Only.
//
// For nested attribute types, applying this plan modifier at root attribute is enough.
type ImmutableFieldModifier struct{}

func (m ImmutableFieldModifier) Modify(ctx context.Context, req tfsdk.ModifyAttributePlanRequest, resp *tfsdk.ModifyAttributePlanResponse) {
	if req.AttributeState == nil || req.AttributeState.IsNull() {
		return
	}

	if !req.AttributeConfig.IsNull() && !req.AttributeConfig.Equal(req.AttributeState) {
		resp.Diagnostics.AddError(
			"Immutable Field Error",
			fmt.Sprintf(
				"Field '%s' is immutable and cannot be updated. Please destroy and recreate the resource if changes are needed.",
				req.AttributePath.String(),
			),
		)
	}
}

func (m ImmutableFieldModifier) Description(ctx context.Context) string {
	return "Errors if the field is changed after resource creation"
}

func (m ImmutableFieldModifier) MarkdownDescription(ctx context.Context) string {
	return "Errors if the field is changed after resource creation"
}
