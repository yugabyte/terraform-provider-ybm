/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"net/http"
	"testing"

	mocks "github.com/yugabyte/terraform-provider-ybm/mock_yugabytedb_managed_go_client_internal"

	gomock "github.com/golang/mock/gomock"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func getListAccountsRequest(ctx context.Context, cfg *openapiclient.Configuration, mockAccountApi *mocks.MockAccountApi) *openapiclient.ApiListAccountsRequest {
	testClient := openapiclient.NewAPIClient(cfg)
	listAccountsRequest := testClient.AccountApi.ListAccounts(ctx)
	listAccountsRequest.ApiService = mockAccountApi
	return &listAccountsRequest
}

func getListAccountsResponse(accountID string) *openapiclient.AccountListResponse {
	listAccountsResponse := openapiclient.NewAccountListResponseWithDefaults()
	accountData := []openapiclient.AccountData{}
	accountDatum := openapiclient.NewAccountDataWithDefaults()
	accountDatum.SetInfo(*openapiclient.NewAccountInfoWithDefaults())
	accountDatum.Info.SetId(accountID)
	accountData = append(accountData, *accountDatum)
	listAccountsResponse.SetData(accountData)
	return listAccountsResponse
}

func getListProjectsRequest(ctx context.Context, cfg *openapiclient.Configuration, accountID string, mockProjectApi *mocks.MockProjectApi) *openapiclient.ApiListProjectsRequest {
	testClient := openapiclient.NewAPIClient(cfg)
	listProjectsRequest := testClient.ProjectApi.ListProjects(ctx, accountID)
	listProjectsRequest.ApiService = mockProjectApi
	return &listProjectsRequest
}

func getListProjectsResponse(projectID string) *openapiclient.ProjectListResponse {
	listProjectsResponse := openapiclient.NewProjectListResponseWithDefaults()
	projectData := []openapiclient.ProjectData{}
	projectDatum := openapiclient.NewProjectDataWithDefaults()
	projectDatum.SetId(projectID)
	projectData = append(projectData, *projectDatum)
	listProjectsResponse.SetData(projectData)
	return listProjectsResponse
}

func TestGetProjectID(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	mockProjectApi := mocks.NewMockProjectApi(mockCtrl)

	cfg := openapiclient.NewConfiguration()
	ctx := context.Background()

	apiClient := openapiclient.NewAPIClient(cfg)
	apiClient.ProjectApi = mockProjectApi

	testCases := []struct {
		TestName          string
		AccountID         string
		ExpectedProjectID string
		ExpectedStatus    bool
		ExpectedError     string
	}{
		{
			TestName:          "Proper Account ID",
			AccountID:         "test-account-id",
			ExpectedProjectID: "test-project-id",
			ExpectedStatus:    true,
			ExpectedError:     "",
		},
	}

	for _, testCase := range testCases {
		accountID := testCase.AccountID
		expectedProjectID := testCase.ExpectedProjectID
		expectedStatus := testCase.ExpectedStatus
		expectedError := testCase.ExpectedError
		t.Run(testCase.TestName, func(t *testing.T) {

			listProjectsRequest := getListProjectsRequest(ctx, cfg, accountID, mockProjectApi)
			listProjectsResponse := getListProjectsResponse(expectedProjectID)
			httpSuccessResponse := &http.Response{
				Status:     "200 OK",
				StatusCode: 200,
			}

			mockProjectApi.EXPECT().ListProjects(ctx, testCase.AccountID).Return(*listProjectsRequest).Times(1)
			mockProjectApi.EXPECT().ListProjectsExecute(*listProjectsRequest).Return(*listProjectsResponse, httpSuccessResponse, nil).Times(1)

			gotProjectID, gotStatus, gotError := getProjectId(ctx, apiClient, testCase.AccountID)
			if gotProjectID != expectedProjectID || gotStatus != expectedStatus || gotError != expectedError {
				t.Errorf("getProjectId(ctx,apiClient,%v) = Project ID: %v,Status: %v,Error: %v; want Project ID: %v,Status: %v,Error: %v",
					testCase.AccountID, gotProjectID, gotStatus, gotError, expectedProjectID, expectedStatus, expectedError)
			}
		})
	}
}
