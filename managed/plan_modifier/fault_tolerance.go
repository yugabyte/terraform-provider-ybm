package planmodifier

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// FaultToleranceNone is the fault tolerance value that YugabyteDB Aeon does not
// allow to be changed on an existing cluster.
const FaultToleranceNone = "NONE"

const faultToleranceRequiresReplaceDescription = "Changing fault_tolerance away from NONE on an existing cluster " +
	"forces the cluster to be destroyed and recreated, because YugabyteDB Aeon does not support this change in place."

// FaultToleranceRequiresReplace returns a plan modifier that marks the
// resource for replacement when fault_tolerance moves from NONE to any other
// value. YugabyteDB Aeon rejects that update in place with
// "Cannot modify the fault tolerance from NONE to <value>" (HTTP 403), so
// planning it as an update only defers the failure to apply time.
//
// Create, destroy, unchanged values and a null config (the attribute is
// Computed) are already skipped by tfsdk.RequiresReplaceIf.
func FaultToleranceRequiresReplace() tfsdk.AttributePlanModifier {
	return tfsdk.RequiresReplaceIf(
		func(_ context.Context, state, config attr.Value, _ path.Path) (bool, diag.Diagnostics) {
			return FaultToleranceChangeRequiresReplace(state, config), nil
		},
		faultToleranceRequiresReplaceDescription,
		faultToleranceRequiresReplaceDescription,
	)
}

// FaultToleranceChangeRequiresReplace reports whether moving fault_tolerance
// from the prior value to the new value cannot be done in place. It returns
// false when either value is null or unknown, so creates and values that are
// only known at apply time are never turned into a replacement.
func FaultToleranceChangeRequiresReplace(prior, planned attr.Value) bool {
	priorValue, ok := knownString(prior)
	if !ok {
		return false
	}
	plannedValue, ok := knownString(planned)
	if !ok {
		return false
	}
	return priorValue == FaultToleranceNone && plannedValue != FaultToleranceNone
}

func knownString(v attr.Value) (string, bool) {
	var s types.String
	switch typed := v.(type) {
	case types.String:
		s = typed
	case *types.String:
		if typed == nil {
			return "", false
		}
		s = *typed
	default:
		return "", false
	}
	if s.IsNull() || s.IsUnknown() {
		return "", false
	}
	return s.Value, true
}
