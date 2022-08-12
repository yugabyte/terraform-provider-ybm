package managed

import (
	"context"
	"net/http/httputil"

	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func getProjectId(accountId string, apiClient *openapiclient.APIClient) (projectId string, projectIdOK bool, errorMessage string) {
	projectResp, resp, err := apiClient.ProjectApi.ListProjects(context.Background(), accountId).Execute()
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

func getAccountId(apiClient *openapiclient.APIClient) (accountId string, accountIdOK bool, errorMessage string) {
	accountResp, resp, err := apiClient.AccountApi.ListAccounts(context.Background()).Execute()
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
