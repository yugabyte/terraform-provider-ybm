package managed

import (
	"context"
	"fmt"
	"net/http/httputil"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func getProjectId(ctx context.Context, apiClient *openapiclient.APIClient, accountId string) (projectId string, projectIdOK bool, errorMessage string) {
	projectResp, resp, err := apiClient.ProjectApi.ListProjects(ctx, accountId).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(resp, true)
		return "", false, string(b)
	}
	projectData := projectResp.GetData()
	if len(projectData) == 0 {
		return "", false, "The account is not associated with any projects."
	}
	if len(projectData) > 1 {
		return "", false, "The account is associated with multiple projects, please provide a project ID."
	}

	projectId = projectData[0].Id
	return projectId, true, ""
}

func getMemoryFromInstanceType(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, cloud string, tier string, region string, numCores int32) (memory int32, memoryOK bool, errorMessage string) {
	instanceResp, resp, err := apiClient.ClusterApi.GetSupportedInstanceTypes(context.Background()).AccountId(accountId).Cloud(cloud).Tier(tier).Region(region).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(resp, true)
		return 0, false, string(b)
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
	instanceResp, resp, err := apiClient.ClusterApi.GetSupportedInstanceTypes(context.Background()).AccountId(accountId).Cloud(cloud).Tier(tier).Region(region).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(resp, true)
		return 0, false, string(b)
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
		b, _ := httputil.DumpResponse(resp, true)
		return "", false, string(b)
	}
	tracksData := tracksResp.GetData()

	for _, track := range tracksData {
		if track.Spec.GetName() == trackName {
			return track.Info.GetId(), true, ""
		}
	}

	return "", false, "The database version doesn't exist."
}

func getAccountId(ctx context.Context, apiClient *openapiclient.APIClient) (accountId string, accountIdOK bool, errorMessage string) {
	accountResp, resp, err := apiClient.AccountApi.ListAccounts(ctx).Execute()
	if err != nil {
		b, _ := httputil.DumpResponse(resp, true)
		return "", false, string(b)
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
