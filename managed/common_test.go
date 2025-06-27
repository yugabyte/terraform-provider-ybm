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

func getCurrentAccountRequest(ctx context.Context, cfg *openapiclient.Configuration, mockAccountApi *mocks.MockAccountApi) *openapiclient.ApiGetCurrentAccountRequest {
	testClient := openapiclient.NewAPIClient(cfg)
	accountRequest := testClient.AccountApi.GetCurrentAccount(ctx)
	accountRequest.ApiService = mockAccountApi
	return &accountRequest
}

func getCurrentAccountResponse(accountID string, projectID string) *openapiclient.AccountResponse {
	accountResponse := openapiclient.NewAccountResponseWithDefaults()
	accountDatum := openapiclient.NewAccountDataWithDefaults()
	accountDatum.SetInfo(*openapiclient.NewAccountInfoWithDefaults())
	accountDatum.Info.SetId(accountID)
	projectData := []openapiclient.ProjectData{}
	projectDatum := openapiclient.NewProjectDataWithDefaults()
	projectDatum.SetInfo(*openapiclient.NewProjectDataInfoWithDefaults())
	projectDatum.Info.SetId(projectID)
	projectData = append(projectData, *projectDatum)
	accountDatum.Info.SetProjects(projectData)
	accountResponse.SetData(*accountDatum)
	return accountResponse
}

func TestGetProjectID(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	mockAccountApi := mocks.NewMockAccountApi(mockCtrl)

	cfg := openapiclient.NewConfiguration()
	ctx := context.Background()

	apiClient := openapiclient.NewAPIClient(cfg)
	apiClient.AccountApi = mockAccountApi

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

			accountRequest := getCurrentAccountRequest(ctx, cfg, mockAccountApi)
			accountResponse := getCurrentAccountResponse(accountID, expectedProjectID)
			httpSuccessResponse := &http.Response{
				Status:     "200 OK",
				StatusCode: 200,
			}

			mockAccountApi.EXPECT().GetCurrentAccount(ctx).Return(*accountRequest).Times(1)
			mockAccountApi.EXPECT().GetCurrentAccountExecute(*accountRequest).Return(*accountResponse, httpSuccessResponse, nil).Times(1)

			gotProjectID, gotStatus, gotError := getProjectId(ctx, apiClient, testCase.AccountID)
			if gotProjectID != expectedProjectID || gotStatus != expectedStatus || gotError != expectedError {
				t.Errorf("getProjectId(ctx,apiClient,%v) = Project ID: %v,Status: %v,Error: %v; want Project ID: %v,Status: %v,Error: %v",
					testCase.AccountID, gotProjectID, gotStatus, gotError, expectedProjectID, expectedStatus, expectedError)
			}
		})
	}
}
