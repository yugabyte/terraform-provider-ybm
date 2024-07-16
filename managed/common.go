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

	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

// Use to differentiate errors
var ErrFailedTask = errors.New("the task failed")

func getProjectId(ctx context.Context, apiClient *openapiclient.APIClient, accountId string) (projectId string, projectIdOK bool, errorMessage string) {
	accountResp, resp, err := apiClient.AccountApi.ListAccounts(ctx).Execute()
	if err != nil {
		errMsg := getErrorMessage(resp, err)
		return "", false, errMsg
	}
	projectData := accountResp.GetData()[0].Info.GetProjects()
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
	instanceResp, resp, err := apiClient.ClusterApi.GetSupportedNodeConfigurations(context.Background()).AccountId(accountId).Cloud(cloud).Tier(tier).Regions([]string{region}).Execute()
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
	instanceResp, resp, err := apiClient.ClusterApi.GetSupportedNodeConfigurations(context.Background()).AccountId(accountId).Cloud(cloud).Tier(tier).Regions([]string{region}).Execute()
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
		if track.Spec.GetName() == trackName || (track.Spec.GetName() == "Production" && trackName == "Stable") {
			return track.Info.GetId(), true, ""
		}
	}

	return "", false, "The database version doesn't exist."
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

func getAccountId(ctx context.Context, apiClient *openapiclient.APIClient) (accountId string, accountIdOK bool, errorMessage string) {
	accountResp, resp, err := apiClient.AccountApi.ListAccounts(ctx).Execute()
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
	accountData := accountResp.GetData()
	if len(accountData) == 0 {
		return "", false, "The user is not associated with any accounts."
	}
	if len(accountData) > 1 {
		return "", false, "The user is associated with multiple accounts, please provide an account ID."
	}
	accountId = accountData[0].Info.Id
	return accountId, true, ""
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
