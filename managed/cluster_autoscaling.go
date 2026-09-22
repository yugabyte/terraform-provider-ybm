/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

const autoscalerDriftWarningSummary = "Set ignore_num_nodes_changes to avoid Terraform drift"
const autoscalerDriftWarningDetail = "When an autoscaler policy is ACTIVE, YugabyteDB Aeon may change the region's node count outside Terraform. " +
	"Set cluster_region_info.ignore_num_nodes_changes = true on the corresponding ybm_cluster region so Terraform does not treat those changes as drift. " +
	"If ignore_num_nodes_changes is already set to true for that region, no action is required. " +
	"See the ybm_cluster ignore_num_nodes_changes attribute and the autoscaler-drift-handling example."

const enableIgnoreNumNodesWithDriftErrorSummary = "Cannot enable ignore_num_nodes_changes while node-count drift exists"
const disableIgnoreNumNodesWithDriftErrorSummary = "Cannot disable ignore_num_nodes_changes while node-count drift exists"

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

// resolveNumNodesForEdit chooses the node count for a cluster edit payload.
// Explicit num_nodes changes and ignore=false both use the configured/plan value.
// When ignore remains true with no explicit num_nodes change, the live YBM count is kept.
func resolveNumNodesForEdit(planRegion RegionInfo, stateRegion RegionInfo, actualNumNodes int32) int32 {
	planIgnoreEnabled := ignoreNumNodesChangesEnabled(planRegion)
	numNodesChanged := planRegion.NumNodes.Value != stateRegion.NumNodes.Value
	if numNodesChanged {
		return int32(planRegion.NumNodes.Value)
	}
	if !planIgnoreEnabled {
		return int32(planRegion.NumNodes.Value)
	}
	return actualNumNodes
}

// ignoreNumNodesDriftDetail builds plan-error detail. action is "enable" or "disable".
func ignoreNumNodesDriftDetail(regionCode string, currentNodes, configuredNodes int64, action string) string {
	return fmt.Sprintf(
		"Region %s currently has %d nodes, while num_nodes is configured as %d. "+
			"Align the node counts before %sing ignore_num_nodes_changes. "+
			"To keep %d nodes, set num_nodes to %d and apply first. To use %d nodes, "+
			"resize the cluster to %d first. Then %s ignore_num_nodes_changes.",
		regionCode,
		currentNodes,
		configuredNodes,
		strings.TrimSuffix(action, "e"),
		currentNodes,
		currentNodes,
		configuredNodes,
		configuredNodes,
		action,
	)
}

// addIgnoreNumNodesDriftErrors errors when ignore_num_nodes_changes is changing and
// current nodes != configured num_nodes. For false→true, live counts come from
// refreshed state. For true→false, liveNumNodesByRegion must supply GetCluster counts.
func addIgnoreNumNodesDriftErrors(
	diags *diag.Diagnostics,
	configRegions []RegionInfo,
	stateRegions []RegionInfo,
	liveNumNodesByRegion map[string]int64,
) {
	if diags == nil {
		return
	}
	stateByRegion := regionInfoByRegion(stateRegions)
	for _, configRegion := range configRegions {
		regionCode := configRegion.Region.Value
		stateRegion, ok := stateByRegion[regionCode]
		if !ok {
			continue
		}

		configIgnore := ignoreNumNodesChangesEnabled(configRegion)
		stateIgnore := ignoreNumNodesChangesEnabled(stateRegion)
		if configIgnore == stateIgnore {
			continue
		}

		configuredNodes := configRegion.NumNodes.Value

		if !stateIgnore && configIgnore {
			currentNodes := stateRegion.NumNodes.Value
			if currentNodes != configuredNodes {
				diags.AddError(
					enableIgnoreNumNodesWithDriftErrorSummary,
					ignoreNumNodesDriftDetail(regionCode, currentNodes, configuredNodes, "enable"),
				)
			}
			continue
		}

		currentNodes, hasLive := liveNumNodesByRegion[regionCode]
		if !hasLive {
			diags.AddError(
				disableIgnoreNumNodesWithDriftErrorSummary,
				fmt.Sprintf(
					"Unable to verify the current node count for region %s while disabling ignore_num_nodes_changes. "+
						"Retry the plan, or set num_nodes to the current YugabyteDB Aeon node count and apply while "+
						"ignore_num_nodes_changes is still enabled before disabling it.",
					regionCode,
				),
			)
			continue
		}
		if currentNodes != configuredNodes {
			diags.AddError(
				disableIgnoreNumNodesWithDriftErrorSummary,
				ignoreNumNodesDriftDetail(regionCode, currentNodes, configuredNodes, "disable"),
			)
		}
	}
}
