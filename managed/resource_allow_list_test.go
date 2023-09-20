/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	mocks "github.com/yugabyte/terraform-provider-ybm/mock_yugabytedb_managed_go_client_internal"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func getMockAllowList(cfg *openapiclient.Configuration, mockNetworkApi *mocks.MockNetworkApi, mockProjectApi *mocks.MockProjectApi, mockAccountApi *mocks.MockAccountApi) *resourceAllowList {

	apiClient := openapiclient.NewAPIClient(cfg)
	apiClient.NetworkApi = mockNetworkApi
	apiClient.ProjectApi = mockProjectApi
	apiClient.AccountApi = mockAccountApi

	p := provider{
		configured: true,
		client:     apiClient,
	}
	allowList := &resourceAllowList{
		p: p,
	}

	return allowList

}

func getCreateAllowListRequest(ctx context.Context, cfg *openapiclient.Configuration, accountID string, projectID string, mockNetworkApi *mocks.MockNetworkApi) *openapiclient.ApiCreateNetworkAllowListRequest {
	testClient := openapiclient.NewAPIClient(cfg)
	createAllowListRequest := testClient.NetworkApi.CreateNetworkAllowList(ctx, accountID, projectID)
	createAllowListRequest.ApiService = mockNetworkApi
	return &createAllowListRequest
}

func getCreateAllowListResponse(allowListID string, projectID string, cidrList []string, allowListDescription string, allowListName string) *openapiclient.NetworkAllowListResponse {
	allowListClusterInfo := []openapiclient.NetworkAllowListClusterInfo{*openapiclient.NewNetworkAllowListClusterInfoWithDefaults()}
	allowListInfo := openapiclient.NewNetworkAllowListInfo(allowListID, projectID, []string{}, allowListClusterInfo)
	allowListSpec := openapiclient.NewNetworkAllowListSpec(allowListName, allowListDescription, cidrList)
	allowListData := openapiclient.NewNetworkAllowListData(*allowListSpec, *allowListInfo)
	createAllowListResponse := openapiclient.NewNetworkAllowListResponse(*allowListData)
	return createAllowListResponse
}

func getGetAllowListRequest(ctx context.Context, cfg *openapiclient.Configuration, accountID string, projectID string, allowListID string, mockNetworkApi *mocks.MockNetworkApi) *openapiclient.ApiGetNetworkAllowListRequest {
	testClient := openapiclient.NewAPIClient(cfg)
	getAllowListRequest := testClient.NetworkApi.GetNetworkAllowList(ctx, accountID, projectID, allowListID)
	getAllowListRequest.ApiService = mockNetworkApi
	return &getAllowListRequest
}

func getListAllowListRequest(ctx context.Context, cfg *openapiclient.Configuration, accountID string, projectID string, mockNetworkApi *mocks.MockNetworkApi) *openapiclient.ApiListNetworkAllowListsRequest {
	testClient := openapiclient.NewAPIClient(cfg)
	listAllowListRequest := testClient.NetworkApi.ListNetworkAllowLists(ctx, accountID, projectID)
	listAllowListRequest.ApiService = mockNetworkApi
	return &listAllowListRequest
}
func getListAllowListResponse(allowListID string, projectID string, cidrList []string, allowListDescription string, allowListName string) *openapiclient.NetworkAllowListListResponse {
	allowListList := openapiclient.NewNetworkAllowListListResponseWithDefaults()
	allowList := getCreateAllowListResponse(allowListID, projectID, cidrList, allowListDescription, allowListName)
	allowListList.SetData([]openapiclient.NetworkAllowListData{allowList.Data})

	return allowListList
}

func getDeleteAllowListRequest(ctx context.Context, cfg *openapiclient.Configuration, accountID string, projectID string, allowListID string, mockNetworkApi *mocks.MockNetworkApi) *openapiclient.ApiDeleteNetworkAllowListRequest {
	testClient := openapiclient.NewAPIClient(cfg)
	deleteAllowListRequest := testClient.NetworkApi.DeleteNetworkAllowList(ctx, accountID, projectID, allowListID)
	deleteAllowListRequest.ApiService = mockNetworkApi
	return &deleteAllowListRequest
}

func TestCreateAllowList(t *testing.T) {

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	mockNetworkApi := mocks.NewMockNetworkApi(mockCtrl)
	mockAccountApi := mocks.NewMockAccountApi(mockCtrl)
	mockProjectApi := mocks.NewMockProjectApi(mockCtrl)
	ctx := context.Background()
	cfg := openapiclient.NewConfiguration()

	accountID := "test-account-id"
	projectID := "test-project-id"
	cidrList := []string{"0.0.0.0/0"}
	cidrListSchema := []types.String{{Value: "0.0.0.0/0"}}
	allowListName := "allow-all"
	allowListDescription := "Allow connections from any IP address"
	allowListID := "test-allow-list-id"
	allowList := getMockAllowList(cfg, mockNetworkApi, mockProjectApi, mockAccountApi)
	listAccountsRequest := getListAccountsRequest(ctx, cfg, mockAccountApi)
	listAccountsResponse := getListAccountsResponse(accountID, projectID)

	createAllowListRequest := getCreateAllowListRequest(ctx, cfg, accountID, projectID, mockNetworkApi)
	createAllowListSpec := *openapiclient.NewNetworkAllowListSpec(allowListName, allowListDescription, cidrList)
	createAllowListRequestFinal := createAllowListRequest.NetworkAllowListSpec(createAllowListSpec)
	createAllowListResponse := getCreateAllowListResponse(allowListID, projectID, cidrList, allowListDescription, allowListName)
	getAllowListRequest := getGetAllowListRequest(ctx, cfg, accountID, projectID, allowListID, mockNetworkApi)
	listNetworkAllowListsRequest := getListAllowListRequest(ctx, cfg, accountID, projectID, mockNetworkApi)
	listNetworkAllowListsResponse := getListAllowListResponse("non-exist", projectID, cidrList, allowListDescription, "non-exist")
	listNetworkAllowListsResponseExist := getListAllowListResponse(allowListID, projectID, cidrList, allowListDescription, allowListName)

	req := tfsdk.CreateResourceRequest{}
	allowListType := resourceAllowListType{}
	schema, _ := allowListType.GetSchema(ctx)
	req.Plan.Schema = schema
	resp := &tfsdk.CreateResourceResponse{}
	resp.State.Schema = schema

	plan := AllowList{
		AllowListName:        types.String{Value: allowListName},
		AllowListDescription: types.String{Value: allowListDescription},
		CIDRList:             cidrListSchema,
		AllowListID:          types.String{Null: true},
	}
	req.Plan.Set(ctx, &plan)

	desiredState := tfsdk.State{}
	desiredState.Schema = schema
	desiredState.Set(ctx, &AllowList{
		AccountID:            types.String{Value: accountID},
		AllowListName:        types.String{Value: allowListName},
		AllowListDescription: types.String{Value: allowListDescription},
		CIDRList:             cidrListSchema,
		AllowListID:          types.String{Value: allowListID},
		ProjectID:            types.String{Value: projectID},
		ClusterIDs:           []types.String{},
	})

	httpSuccessResponse := &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
	}

	testCases := []struct {
		TestName      string
		ExpectedState tfsdk.State
	}{
		{
			TestName:      "Proper Input",
			ExpectedState: desiredState,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.TestName, func(t *testing.T) {

			mockAccountApi.EXPECT().ListAccounts(ctx).Return(*listAccountsRequest).Times(2)
			mockAccountApi.EXPECT().ListAccountsExecute(*listAccountsRequest).Return(*listAccountsResponse, httpSuccessResponse, nil).Times(2)
			mockNetworkApi.EXPECT().ListNetworkAllowLists(ctx, accountID, projectID).Return(*listNetworkAllowListsRequest).Times(1)
			mockNetworkApi.EXPECT().ListNetworkAllowListsExecute(*listNetworkAllowListsRequest).Return(*listNetworkAllowListsResponse, httpSuccessResponse, nil).Times(1)
			mockNetworkApi.EXPECT().CreateNetworkAllowList(ctx, accountID, projectID).Return(*createAllowListRequest).Times(1)
			mockNetworkApi.EXPECT().CreateNetworkAllowListExecute(createAllowListRequestFinal).Return(*createAllowListResponse, httpSuccessResponse, nil).Times(1)
			mockNetworkApi.EXPECT().ListNetworkAllowLists(ctx, accountID, projectID).Return(*listNetworkAllowListsRequest).Times(1)
			mockNetworkApi.EXPECT().ListNetworkAllowListsExecute(*listNetworkAllowListsRequest).Return(*listNetworkAllowListsResponseExist, httpSuccessResponse, nil).Times(1)
			mockNetworkApi.EXPECT().GetNetworkAllowList(ctx, accountID, projectID, allowListID).Return(*getAllowListRequest).Times(1)
			mockNetworkApi.EXPECT().GetNetworkAllowListExecute(*getAllowListRequest).Return(*createAllowListResponse, httpSuccessResponse, nil).Times(1)
			allowList.Create(ctx, req, resp)

			if !reflect.DeepEqual(resp.State, testCase.ExpectedState) {
				t.Errorf("Got State: %v, Expected State: %v", resp.State, testCase.ExpectedState)
			}
		})

	}
}

func TestReadAllowList(t *testing.T) {

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	mockNetworkApi := mocks.NewMockNetworkApi(mockCtrl)
	mockProjectApi := mocks.NewMockProjectApi(mockCtrl)
	mockAccountApi := mocks.NewMockAccountApi(mockCtrl)
	ctx := context.Background()
	cfg := openapiclient.NewConfiguration()

	accountID := "test-account-id"
	projectID := "test-project-id"
	cidrList := []string{"0.0.0.0/0"}
	cidrListSchema := []types.String{{Value: "0.0.0.0/0"}}
	allowListName := "allow-all"
	allowListDescription := "Allows all the IP addresses"
	allowListID := "test-allow-list-id"
	allowList := getMockAllowList(cfg, mockNetworkApi, mockProjectApi, mockAccountApi)
	createAllowListResponse := getCreateAllowListResponse(allowListID, projectID, cidrList, allowListDescription, allowListName)
	getAllowListRequest := getGetAllowListRequest(ctx, cfg, accountID, projectID, allowListID, mockNetworkApi)
	listNetworkAllowListsRequest := getListAllowListRequest(ctx, cfg, accountID, projectID, mockNetworkApi)
	listNetworkAllowListsResponseExist := getListAllowListResponse(allowListID, projectID, cidrList, allowListDescription, allowListName)

	req := tfsdk.ReadResourceRequest{}
	allowListType := resourceAllowListType{}
	schema, _ := allowListType.GetSchema(ctx)
	req.State.Schema = schema
	inputState := &AllowList{
		AccountID:            types.String{Value: accountID},
		AllowListName:        types.String{Value: allowListName},
		AllowListDescription: types.String{Value: allowListDescription},
		CIDRList:             cidrListSchema,
		AllowListID:          types.String{Value: allowListID},
		ProjectID:            types.String{Value: projectID},
		ClusterIDs:           []types.String{},
	}
	req.State.Set(ctx, inputState)
	resp := &tfsdk.ReadResourceResponse{}
	resp.State.Schema = schema

	desiredState := tfsdk.State{}
	desiredState.Schema = schema
	desiredState.Set(ctx, inputState)

	httpSuccessResponse := &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
	}

	testCases := []struct {
		TestName      string
		ExpectedState tfsdk.State
	}{
		{
			TestName:      "Proper Input",
			ExpectedState: desiredState,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.TestName, func(t *testing.T) {

			listProjectsRequest := getListAccountsRequest(ctx, cfg, mockAccountApi)
			listProjectsResponse := getListAccountsResponse(accountID, "test-project-id")
			mockAccountApi.EXPECT().ListAccounts(ctx).Return(*listProjectsRequest).Times(2)
			mockAccountApi.EXPECT().ListAccountsExecute(*listProjectsRequest).Return(*listProjectsResponse, httpSuccessResponse, nil).Times(2)
			mockNetworkApi.EXPECT().ListNetworkAllowLists(ctx, accountID, projectID).Return(*listNetworkAllowListsRequest).Times(1)
			mockNetworkApi.EXPECT().ListNetworkAllowListsExecute(*listNetworkAllowListsRequest).Return(*listNetworkAllowListsResponseExist, httpSuccessResponse, nil).Times(1)
			mockNetworkApi.EXPECT().GetNetworkAllowList(ctx, accountID, projectID, allowListID).Return(*getAllowListRequest).Times(1)
			mockNetworkApi.EXPECT().GetNetworkAllowListExecute(*getAllowListRequest).Return(*createAllowListResponse, httpSuccessResponse, nil).Times(1)
			allowList.Read(ctx, req, resp)

			if !reflect.DeepEqual(resp.State, testCase.ExpectedState) {
				t.Errorf("Got State: %v, Expected State: %v", resp.State, testCase.ExpectedState)
			}
		})

	}
}

func TestUpdateAllowList(t *testing.T) {

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	mockNetworkApi := mocks.NewMockNetworkApi(mockCtrl)
	mockProjectApi := mocks.NewMockProjectApi(mockCtrl)
	mockAccountApi := mocks.NewMockAccountApi(mockCtrl)
	ctx := context.Background()
	cfg := openapiclient.NewConfiguration()
	allowList := getMockAllowList(cfg, mockNetworkApi, mockProjectApi, mockAccountApi)
	req := tfsdk.UpdateResourceRequest{}
	resp := &tfsdk.UpdateResourceResponse{}
	diags := diag.Diagnostics{}
	diags.AddError("Unable to update allow list", "Updating allow lists is not currently supported. Delete and recreate the provider.")

	testCases := []struct {
		TestName            string
		ExpectedDiagnostics diag.Diagnostics
	}{
		{
			TestName:            "Update allow list",
			ExpectedDiagnostics: diags,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.TestName, func(t *testing.T) {

			allowList.Update(ctx, req, resp)

			if !reflect.DeepEqual(resp.Diagnostics, testCase.ExpectedDiagnostics) {
				t.Errorf("Got Diagnostics: %v, Expected Diagnostics: %v", resp.Diagnostics, testCase.ExpectedDiagnostics)
			}
		})

	}
}

func TestDeleteAllowList(t *testing.T) {

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	mockNetworkApi := mocks.NewMockNetworkApi(mockCtrl)
	mockProjectApi := mocks.NewMockProjectApi(mockCtrl)
	mockAccountApi := mocks.NewMockAccountApi(mockCtrl)
	ctx := context.Background()
	cfg := openapiclient.NewConfiguration()

	accountID := "test-account-id"
	projectID := "test-project-id"
	allowListID := "test-allow-list-id"
	allowList := getMockAllowList(cfg, mockNetworkApi, mockProjectApi, mockAccountApi)
	deleteAllowListRequest := getDeleteAllowListRequest(ctx, cfg, accountID, projectID, allowListID, mockNetworkApi)

	req := tfsdk.DeleteResourceRequest{}
	allowListType := resourceAllowListType{}
	schema, _ := allowListType.GetSchema(ctx)
	req.State.Schema = schema
	inputState := &AllowList{
		AccountID:   types.String{Value: accountID},
		AllowListID: types.String{Value: allowListID},
		ProjectID:   types.String{Value: projectID},
		ClusterIDs:  []types.String{},
	}
	req.State.Set(ctx, inputState)

	resp := &tfsdk.DeleteResourceResponse{}
	resp.State.Schema = schema

	desiredState := tfsdk.State{}
	desiredState.Schema = schema
	desiredState.RemoveResource(ctx)

	httpSuccessResponse := &http.Response{
		Status:     "200 OK",
		StatusCode: 200,
	}

	testCases := []struct {
		TestName      string
		ExpectedState tfsdk.State
	}{
		{
			TestName:      "Proper Input",
			ExpectedState: desiredState,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.TestName, func(t *testing.T) {
			mockNetworkApi.EXPECT().DeleteNetworkAllowList(ctx, accountID, projectID, allowListID).Return(*deleteAllowListRequest).Times(1)
			mockNetworkApi.EXPECT().DeleteNetworkAllowListExecute(*deleteAllowListRequest).Return(httpSuccessResponse, nil).Times(1)
			allowList.Delete(ctx, req, resp)
			if !reflect.DeepEqual(resp.State, testCase.ExpectedState) {
				t.Errorf("Got State: %v, Expected State: %v", resp.State, testCase.ExpectedState)
			}
		})
	}
}

func TestImportStateAllowList(t *testing.T) {
	// Test case will be added later once the feature is implemented
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	mockNetworkApi := mocks.NewMockNetworkApi(mockCtrl)
	mockProjectApi := mocks.NewMockProjectApi(mockCtrl)
	mockAccountApi := mocks.NewMockAccountApi(mockCtrl)
	ctx := context.Background()
	cfg := openapiclient.NewConfiguration()
	req := tfsdk.ImportResourceStateRequest{}
	resp := &tfsdk.ImportResourceStateResponse{}
	allowList := getMockAllowList(cfg, mockNetworkApi, mockProjectApi, mockAccountApi)
	allowList.ImportState(ctx, req, resp)
}
