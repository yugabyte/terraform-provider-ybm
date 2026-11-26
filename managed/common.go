/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

// Use to differentiate errors
var ErrFailedTask = errors.New("the task failed")

const minClusterRunningTaskOperationTimeout = 30 * time.Minute

func clusterRunningTaskOperationTimeout(numNodes int) time.Duration {
	if numNodes < 1 {
		numNodes = 1
	}
	timeout := time.Duration(15*numNodes) * time.Minute
	if timeout < minClusterRunningTaskOperationTimeout {
		return minClusterRunningTaskOperationTimeout
	}
	return timeout
}

func getClusterTotalNodeCount(ctx context.Context, accountId, projectId, clusterId string, apiClient *openapiclient.APIClient) (int, error) {
	clusterResp, resp, err := apiClient.ClusterApi.GetCluster(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		return 0, errors.New(getErrorMessage(resp, err))
	}
	totalNodes := 0
	for _, regionInfo := range clusterResp.Data.Spec.ClusterRegionInfo {
		totalNodes += int(regionInfo.PlacementInfo.GetNumNodes())
	}
	if totalNodes < 1 {
		totalNodes = 1
	}
	return totalNodes, nil
}

func clusterRunningTaskOperationTimeoutForCluster(ctx context.Context, accountId, projectId, clusterId string, apiClient *openapiclient.APIClient) time.Duration {
	numNodes, err := getClusterTotalNodeCount(ctx, accountId, projectId, clusterId, apiClient)
	if err != nil {
		tflog.Warn(ctx, "Unable to get cluster node count for operation timeout, using minimum timeout: "+err.Error())
		return minClusterRunningTaskOperationTimeout
	}
	return clusterRunningTaskOperationTimeout(numNodes)
}

func clusterRunningTaskRetryPolicy(ctx context.Context, accountId, projectId, clusterId string, apiClient *openapiclient.APIClient) retry.Backoff {
	timeout := clusterRunningTaskOperationTimeoutForCluster(ctx, accountId, projectId, clusterId, apiClient)
	return retry.WithMaxDuration(timeout, retry.NewConstant(10*time.Second))
}

func getProjectId(ctx context.Context, apiClient *openapiclient.APIClient, accountId string) (projectId string, projectIdOK bool, errorMessage string) {
	accountResp, resp, err := apiClient.AccountApi.GetCurrentAccount(ctx).Execute()
	if err != nil {
		errMsg := getErrorMessage(resp, err)
		return "", false, errMsg
	}
	projectData := accountResp.Data.Info.GetProjects()
	if len(projectData) == 0 {
		return "", false, "The account is not associated with any projects."
	}
	if len(projectData) > 1 {
		return "", false, "The account is associated with multiple projects, please provide a project ID."
	}

	projectId = projectData[0].Info.Id
	return projectId, true, ""
}

func getMemoryFromInstanceType(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, cloud string, tier string, region string, numCores int32) (memory int32, memoryOK bool, errorMessage string) {
	instanceResp, resp, err := apiClient.ClusterApi.GetSupportedNodeConfigurationsByAccount(context.Background(), accountId).Cloud(cloud).Tier(tier).Regions([]string{region}).Execute()
	if err != nil {
		errMsg := getErrorMessage(resp, err)
		return 0, false, errMsg
	}
	instanceData := instanceResp.GetData()
	nodeConfigList, ok := instanceData[region]
	if !ok || len(nodeConfigList) == 0 {
		return 0, false, "No instances configured for the given region."
	}
	for _, nodeConfig := range nodeConfigList {
		if nodeConfig.GetNumCores() == numCores {
			memory = nodeConfig.GetMemoryMb()
			tflog.Debug(ctx, fmt.Sprintf("Found an instance type with %v cores and %v MB memory in %v cloud in the region %v", numCores, memory, cloud, region))
			return memory, true, ""
		}
	}

	tflog.Debug(ctx, fmt.Sprintf("Could not find a instance with %v cores in %v cloud in the region %v", numCores, cloud, region))

	return 0, false, "Node with the given number of CPU cores doesn't exist in the given region."
}

func getDiskSizeFromInstanceType(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, cloud string, tier string, region string, numCores int32) (diskSize int32, diskSizeOK bool, errorMessage string) {
	instanceResp, resp, err := apiClient.ClusterApi.GetSupportedNodeConfigurationsByAccount(context.Background(), accountId).Cloud(cloud).Tier(tier).Regions([]string{region}).Execute()
	if err != nil {
		errMsg := getErrorMessage(resp, err)
		return 0, false, errMsg
	}
	instanceData := instanceResp.GetData()
	nodeConfigList, ok := instanceData[region]
	if !ok || len(nodeConfigList) == 0 {
		return 0, false, "No instances configured for the given region."
	}
	for _, nodeConfig := range nodeConfigList {
		if nodeConfig.GetNumCores() == numCores {
			diskSize = nodeConfig.GetIncludedDiskSizeGb()
			tflog.Debug(ctx, fmt.Sprintf("Found an instance type with %v cores and %v GB disk size in %v cloud in the region %v", numCores, diskSize, cloud, region))
			return diskSize, true, ""
		}
	}

	tflog.Debug(ctx, fmt.Sprintf("Could not find a instance with %v cores in %v cloud in the region %v", numCores, cloud, region))

	return 0, false, "Node with the given number of CPU cores doesn't exist in the given region."
}

func getTrackId(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, trackName string) (trackId string, trackIdOK bool, errorMessage string) {
	tracksResp, resp, err := apiClient.SoftwareReleaseApi.ListTracks(ctx, accountId).Execute()
	if err != nil {
		errMsg := getErrorMessage(resp, err)
		return "", false, errMsg
	}
	tracksData := tracksResp.GetData()

	for _, track := range tracksData {
		tflog.Debug(ctx, fmt.Sprintf("Required track name:  %v, current track name: %v", track.Spec.GetName(), trackName))
		if track.Spec.GetName() == trackName ||
			(track.Spec.GetName() == "Extended" && (trackName == "Production" || trackName == "Innovation")) ||
			(track.Spec.GetName() == "Rapid" && (trackName == "Preview" || trackName == "Early Access")) {
			return track.Info.GetId(), true, ""
		}
	}

	return "", false, "The database track doesn't exist."
}

func getTrackName(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, trackId string) (trackName string, trackNameOK bool, errorMessage string) {

	trackNameResp, resp, err := apiClient.SoftwareReleaseApi.GetTrack(ctx, accountId, trackId).Execute()
	if err != nil {
		errMsg := getErrorMessage(resp, err)
		return "", false, errMsg
	}
	trackData := trackNameResp.GetData()
	trackName = trackData.Spec.GetName()

	return trackName, true, ""
}

func databaseTrackPriorValue(track types.String) string {
	if track.IsNull() || track.IsUnknown() {
		return ""
	}
	return track.ValueString()
}

// databaseTrackNameForState returns the value to store in Terraform state for database_track.
// When priorTrackName is a deprecated alias (e.g. Production) that maps to the same track ID as
// the cluster's current track, the prior name is preserved so config and state stay aligned.
func databaseTrackNameForState(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, apiTrackID string, priorTrackName string) (trackName string, trackNameOK bool, errorMessage string) {
	apiTrackName, ok, msg := getTrackName(ctx, apiClient, accountId, apiTrackID)
	if !ok {
		return "", false, msg
	}
	if priorTrackName != "" {
		if priorTrackID, priorOK, _ := getTrackId(ctx, apiClient, accountId, priorTrackName); priorOK && priorTrackID == apiTrackID {
			return priorTrackName, true, ""
		}
	}
	return apiTrackName, true, ""
}

func getAccountId(ctx context.Context, apiClient *openapiclient.APIClient) (accountId string, accountIdOK bool, errorMessage string) {
	accountResp, resp, err := apiClient.AccountApi.GetCurrentAccount(ctx).Execute()
	if err != nil {
		errMsg := getErrorMessage(resp, err)
		if strings.Contains(err.Error(), "is not a valid") {
			tflog.Warn(ctx, "The deserialization of the response failed due to following error. "+
				"Skipping as this should not impact the functionality of the provider.",
				map[string]interface{}{"errMsg": err.Error()})
		} else {
			return "", false, errMsg
		}
	}
	return accountResp.Data.Info.Id, true, ""
}

// Utils functions

// GetApiErrorDetails will return the api Error message if present
// If not present will return the original err.Error()
func GetApiErrorDetails(err error) string {
	switch castedError := err.(type) {
	case openapiclient.GenericOpenAPIError:
		if v := getAPIError(castedError.Body()); v != nil {
			if d, ok := v.GetErrorOk(); ok {
				return fmt.Sprintf("%s%s", d.GetDetail(), "\n")
			}
		}
	}
	return err.Error()

}

func getAPIError(b []byte) *openapiclient.ApiError {
	apiError := openapiclient.NewApiErrorWithDefaults()
	err := json.Unmarshal(b, &apiError)
	if err != nil {
		return nil
	}
	return apiError
}

func GetIntegrationDataByName(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, integrationName string) (*openapiclient.TelemetryProviderData, error) {
	integrationConfig, _, err := apiClient.TelemetryProviderApi.
		ListTelemetryProviders(ctx, accountId, projectId).
		Name(integrationName).
		Execute()

	if err != nil {
		return nil, errors.New(GetApiErrorDetails(err))
	}

	if len(integrationConfig.GetData()) < 1 {
		return nil, fmt.Errorf("Integration %s not found", integrationName)
	}

	return &integrationConfig.GetData()[0], nil
}

func GetIntegrationByID(accountId string, projectId string, integrationId string, apiClient *openapiclient.APIClient) (*openapiclient.TelemetryProviderData, error) {
	resp, _, err := apiClient.TelemetryProviderApi.ListTelemetryProviders(context.Background(), accountId, projectId).Limit(100).Execute()

	if err != nil {
		return nil, errors.New(GetApiErrorDetails(err))
	}

	for _, tpData := range resp.Data {
		if tpData.GetInfo().Id == integrationId {
			return &tpData, nil
		}
	}

	return nil, fmt.Errorf("Could not find integration with id: %s", integrationId)
}

// recordCreatedResource saves a resource to state as soon as the API has created it, before any
// waiting or follow-up configuration, so that a later failure in Create does not leave an object
// that exists but that Terraform does not track. The state is the planned values, with unknown
// (computed) values set to null, plus the given attributes (normally the IDs Read needs). The next
// refresh fills in the rest. If Create later returns an error, Terraform keeps the resource in
// state marked as tainted.
//
// It reports whether the state was recorded. It never adds an error: if recording fails, Create
// should still carry on and set the final state, so the failure is only reported as a warning.
// Errors already on resp from earlier steps do not affect it.
func recordCreatedResource(ctx context.Context, plan tfsdk.Plan, resp *resource.CreateResponse, kind string, id string, attrs map[string]attr.Value) bool {
	raw, err := tftypes.Transform(plan.Raw, func(_ *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if !v.IsKnown() {
			return tftypes.NewValue(v.Type(), nil), nil
		}
		return v, nil
	})
	var diags diag.Diagnostics
	if err != nil {
		diags.AddError("Unable to null the unknown planned values", err.Error())
	}
	// Build the state on a copy, so that resp.State is either fully recorded or left untouched.
	state := tfsdk.State{Schema: resp.State.Schema, Raw: raw}
	if !diags.HasError() {
		for name, value := range attrs {
			diags.Append(state.SetAttribute(ctx, path.Root(name), value)...)
		}
	}
	if diags.HasError() {
		tflog.Warn(ctx, fmt.Sprintf("Unable to record %s %s in state right after creation: %v", kind, id, diags))
		resp.Diagnostics.AddWarning(fmt.Sprintf("%s %s could not be recorded in state right after creation", kind, id),
			"If a later step of the creation fails, Terraform will not track it, and it may need to be imported or deleted manually.")
		return false
	}
	resp.State = state
	return true
}

// warnIfCreateIncomplete explains, when Create fails after recordCreatedResource, that the
// resource exists and is tracked but tainted, and how to keep it instead of replacing it. Defer it
// right after recordCreatedResource returns true.
func warnIfCreateIncomplete(resp *resource.CreateResponse, kind string, id string) {
	if !resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning(kind+" was created but not fully configured",
		fmt.Sprintf("%s %s was created and is recorded in the Terraform state, but a later step failed, so "+
			"Terraform has marked it as tainted and will replace it on the next apply. If it is healthy and you "+
			"only want to retry the failed step, run `terraform untaint <resource address>` and then `terraform apply`.", kind, id))
}
