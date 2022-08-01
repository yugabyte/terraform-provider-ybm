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
	projectId = projectResp.GetData()[0].Id
	return projectId, true, ""
}
