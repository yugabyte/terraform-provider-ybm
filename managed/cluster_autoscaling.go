/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

const autoscalerDriftWarningSummary = "Set ignore_num_nodes_changes to avoid Terraform drift"
const autoscalerDriftWarningDetail = "When an autoscaler policy is ACTIVE, YugabyteDB Aeon may change the region's node count outside Terraform. " +
	"Set cluster_region_info.ignore_num_nodes_changes = true on the corresponding ybm_cluster region so Terraform does not treat those changes as drift. " +
	"See the ybm_cluster ignore_num_nodes_changes attribute and the autoscaler-drift-handling example."

func ignoreNumNodesChangesEnabled(region RegionInfo) bool {
	return !region.IgnoreNumNodesChanges.IsNull() &&
		!region.IgnoreNumNodesChanges.IsUnknown() &&
		region.IgnoreNumNodesChanges.Value
}

// appendAutoscalerDriftWarning warns customers to set ignore_num_nodes_changes when
// enabling an autoscaler policy, so they learn about drift prevention before it happens.
func appendAutoscalerDriftWarning(diags *diag.Diagnostics, status string) {
	if diags == nil || !strings.EqualFold(status, "ACTIVE") {
		return
	}

	diags.AddWarning(autoscalerDriftWarningSummary, autoscalerDriftWarningDetail)
}

func regionInfoByRegion(regions []RegionInfo) map[string]RegionInfo {
	result := make(map[string]RegionInfo, len(regions))
	for _, region := range regions {
		result[region.Region.Value] = region
	}
	return result
}

// applyIgnoredNumNodesChanges keeps Terraform's managed num_nodes in state when
// ignore_num_nodes_changes is enabled, instead of writing the runtime YBM value.
// managedRegions should be state on Read and plan on Create/Update so intentional
// config changes (e.g. 3 -> 6) are persisted after apply.
func applyIgnoredNumNodesChanges(cluster *Cluster, managedRegions []RegionInfo) {
	managedByRegion := regionInfoByRegion(managedRegions)

	for i := range cluster.ClusterRegionInfo {
		regionCode := cluster.ClusterRegionInfo[i].Region.Value
		managedRegion, ok := managedByRegion[regionCode]
		if !ok {
			continue
		}

		cluster.ClusterRegionInfo[i].IgnoreNumNodesChanges = managedRegion.IgnoreNumNodesChanges
		if !ignoreNumNodesChangesEnabled(managedRegion) {
			continue
		}

		cluster.ClusterRegionInfo[i].NumNodes = managedRegion.NumNodes
	}
}

func resolveNumNodesForEdit(planRegion RegionInfo, stateRegion RegionInfo, actualNumNodes int32) int32 {
	if !ignoreNumNodesChangesEnabled(planRegion) {
		return int32(planRegion.NumNodes.Value)
	}

	if planRegion.NumNodes.Value != stateRegion.NumNodes.Value {
		return int32(planRegion.NumNodes.Value)
	}

	return actualNumNodes
}
