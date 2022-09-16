package managed

import (
	"context"
	"net/http/httputil"

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
		return 0, false, "No nodes configured for the given region."
	}
	for _, nodeConfig := range nodeConfigList {
		if nodeConfig.GetNumCores() == numCores {
			memory = nodeConfig.GetMemoryMb()
			return memory, true, ""
		}
	}
	return 0, false, "Node with the given number of CPU cores doesn't exist in the given region."
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
