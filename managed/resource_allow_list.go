/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	"github.com/yugabyte/terraform-provider-ybm/managed/util"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceAllowListType struct{}

func (r resourceAllowListType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create an allow list in YugabyteDB Aeon.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this allow list belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this allow list belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"allow_list_id": {
				Description:   "The ID of the allow list. Created automatically when an allow list is created. Use this ID to get a specific allow list.",
				Type:          types.StringType,
				Computed:      true,
				Optional:      true,
				PlanModifiers: tfsdk.AttributePlanModifiers{tfsdk.RequiresReplace()},
			},
			"allow_list_name": {
				Description:   "The name of the allow list.",
				Type:          types.StringType,
				Required:      true,
				PlanModifiers: tfsdk.AttributePlanModifiers{tfsdk.RequiresReplace()},
			},
			"allow_list_description": {
				Description:   "The description of the allow list.",
				Type:          types.StringType,
				Optional:      true,
				PlanModifiers: tfsdk.AttributePlanModifiers{tfsdk.RequiresReplace()},
			},
			"cidr_list": {
				Description: "The CIDR list of the allow list.",
				Type: types.SetType{
					ElemType: types.StringType,
				},
				Required:      true,
				PlanModifiers: tfsdk.AttributePlanModifiers{tfsdk.RequiresReplace()},
			},
			"cluster_ids": {
				Description: "List of the IDs of the clusters the allow list is assigned to.",
				Type: types.SetType{
					ElemType: types.StringType,
				},
				Computed: true,
			},
		},
	}, nil
}

func (r resourceAllowListType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceAllowList{
		p: *(p.(*provider)),
	}, nil
}

type resourceAllowList struct {
	p provider
}

func getAllowListPlan(ctx context.Context, plan tfsdk.Plan, allowList *AllowList) diag.Diagnostics {
	// NOTE: currently must manually fill out each attribute due to usage of Go structs
	// Once the opt-in conversion of null or unknown values to the empty value is implemented, this can all be replaced with req.Plan.Get(ctx, &allowList)
	// See https://www.terraform.io/plugin/framework/accessing-values#conversion-rules
	// I tried implementing Unknownable instead but could not get it to work.
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("allow_list_id"), &allowList.AllowListID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("allow_list_description"), &allowList.AllowListDescription)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("allow_list_name"), &allowList.AllowListName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cidr_list"), &allowList.CIDRList)...)

	return diags
}

// Create allow list
func (r resourceAllowList) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan AllowList
	var accountId, message string
	var getAccountOK bool
	resp.Diagnostics.Append(getAllowListPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the allow list")
		return
	}

	apiClient := r.p.client

	accountId, getAccountOK, message = getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	if (!plan.AllowListID.Unknown && !plan.AllowListID.Null) || plan.AllowListID.Value != "" {
		resp.Diagnostics.AddError(
			"Allow list ID provided for new allow list",
			"The allow_list_id was provided even though a new allow list is being created. Do not include this field in the provider when creating an allow list.",
		)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID ", message)
		return
	}

	allowListName := plan.AllowListName.Value
	allowListDesc := plan.AllowListDescription.Value
	cidrList := []string{}
	for i := range plan.CIDRList {
		cidrList = append(cidrList, plan.CIDRList[i].Value)
	}

	allowListListResp, _, err := apiClient.NetworkApi.ListNetworkAllowLists(ctx, accountId, projectId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to create allow list", err.Error())
		return
	}

	err = findDuplicateNetworkAllowList(allowListListResp.GetData(), allowListName)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create allow list", err.Error())
		return
	}

	networkAllowListSpec := *openapiclient.NewNetworkAllowListSpec(allowListName, allowListDesc, cidrList) // NetworkAllowListSpec | Allow list specification (optional)

	_, response, err := apiClient.NetworkApi.CreateNetworkAllowList(ctx, accountId, projectId).NetworkAllowListSpec(networkAllowListSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create allow list ", errMsg)
		return
	}

	allowList, readOK, message := resourceAllowListRead(accountId, projectId, allowListName, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the allow list ", message)
		return
	}
	tflog.Debug(ctx, "Allow List Create: Allow list on read from API server", map[string]interface{}{
		"Allow List": allowList})

	diags := resp.State.Set(ctx, &allowList)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getIDsFromAllowListState(ctx context.Context, state tfsdk.State, allowList *AllowList) {
	state.GetAttribute(ctx, path.Root("account_id"), &allowList.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &allowList.ProjectID)
	state.GetAttribute(ctx, path.Root("cidr_list"), &allowList.CIDRList)
	state.GetAttribute(ctx, path.Root("allow_list_id"), &allowList.AllowListID)
	state.GetAttribute(ctx, path.Root("allow_list_name"), &allowList.AllowListName)
	state.GetAttribute(ctx, path.Root("cluster_ids"), &allowList.ClusterIDs)
}

func resourceAllowListRead(accountId string, projectId string, allowListName string, apiClient *openapiclient.APIClient) (allowList AllowList, readOK bool, errorMessage string) {

	allowListId, err := getNetworkAllowListIdByName(context.Background(), accountId, projectId, allowListName, *apiClient)
	if err != nil {
		return allowList, false, err.Error()
	}
	allowListResp, response, err := apiClient.NetworkApi.GetNetworkAllowList(context.Background(), accountId, projectId, allowListId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return allowList, false, errMsg
	}

	allowList.AccountID.Value = accountId
	allowList.ProjectID.Value = projectId
	allowList.AllowListID.Value = allowListId

	allowList.AllowListName.Value = allowListResp.Data.Spec.Name
	allowList.AllowListDescription.Value = allowListResp.Data.Spec.Description

	cidrList := []types.String{}
	for _, elem := range allowListResp.Data.Spec.AllowList {
		cidrList = append(cidrList, types.String{Value: elem})
	}
	allowList.CIDRList = cidrList

	clusterIDs := []types.String{}
	for _, elem := range allowListResp.Data.Info.ClusterIds {
		clusterIDs = append(clusterIDs, types.String{Value: elem})
	}
	allowList.ClusterIDs = clusterIDs

	return allowList, true, ""
}

// Read allow list
func (r resourceAllowList) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state AllowList
	getIDsFromAllowListState(ctx, req.State, &state)

	var accountId, projectId, message string
	var getAccountOK bool
	apiClient := r.p.client
	accountId, getAccountOK, message = getAccountId(context.Background(), apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("unable to get account ID", message)
		return
	}

	projectId, getProjectOK, message := getProjectId(context.Background(), apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("unable to get project ID", message)
		return
	}

	cidrList := []string{}
	for i := range state.CIDRList {
		cidrList = append(cidrList, state.CIDRList[i].Value)
	}

	tflog.Debug(ctx, "Allow List Read: CIDR List from state", map[string]interface{}{
		"CIDR List": state.CIDRList})

	allowList, readOK, message := resourceAllowListRead(accountId, projectId, state.AllowListName.Value, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the allow list ", message)
		return
	}
	tflog.Debug(ctx, "Allow List Read: Allow list on read from API server", map[string]interface{}{
		"Allow List": allowList})

	diags := resp.State.Set(ctx, &allowList)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update allow list
func (r resourceAllowList) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {

	resp.Diagnostics.AddError("Unable to update allow list", "Updating allow lists is not currently supported. Delete and recreate the provider.")
	return

}

// Delete allow list
func (r resourceAllowList) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state AllowList
	getIDsFromAllowListState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	allowListId := state.AllowListID.Value
	clusterIds := util.SliceTypesStringToSliceString(state.ClusterIDs)

	apiClient := r.p.client

	//First we remove from cluster
	for _, clusterId := range clusterIds {
		err := removeAllowListFromCluster(context.Background(), accountId, projectId, clusterId, allowListId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Unable to delete the allow list ", err.Error())
			return
		}
	}

	_, err := apiClient.NetworkApi.DeleteNetworkAllowList(context.Background(), accountId, projectId, allowListId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete the allow list ", GetApiErrorDetails(err))
		return
	}

	resp.State.RemoveResource(ctx)

}

// Import allow list
func (r resourceAllowList) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {

	resp.State.SetAttribute(ctx, path.Root("allow_list_name"), req.ID)
}

func findDuplicateNetworkAllowList(nals []openapiclient.NetworkAllowListData, name string) error {

	if _, ok := findNetworkAllowList(nals, name); ok {
		return fmt.Errorf("NetworkAllowList %v already exists", name)
	}
	return nil
}

func findNetworkAllowList(nals []openapiclient.NetworkAllowListData, name string) (openapiclient.NetworkAllowListData, bool) {
	for _, allowList := range nals {
		if allowList.Spec.Name == name {
			return allowList, true
		}
	}
	return openapiclient.NetworkAllowListData{}, false
}

func getNetworkAllowListIdByName(ctx context.Context, accountId string, projectId string, networkAllowListName string, apiClient openapiclient.APIClient) (string, error) {
	var continuationToken string
	for {
		request := apiClient.NetworkApi.ListNetworkAllowLists(ctx, accountId, projectId)
		if continuationToken != "" {
			request = request.ContinuationToken(continuationToken)
		}
		nalResp, resp, err := request.Execute()
		if err != nil {
			errMsg := getErrorMessage(resp, err)
			return "", fmt.Errorf("Unable to read the Network allow list %s: %s", networkAllowListName, errMsg)
		}
		if nalData, ok := findNetworkAllowList(nalResp.Data, networkAllowListName); ok {
			return nalData.Info.GetId(), nil
		}
		continuationToken = nalResp.Metadata.GetContinuationToken()
		if continuationToken == "" {
			break
		}
	}

	return "", fmt.Errorf("NetworkAllowList %s not found", networkAllowListName)

}

func removeAllowListFromCluster(ctx context.Context, accountId string, projectId string, clusterId string, allowListId string, apiClient *openapiclient.APIClient) error {

	clusterResp, resp, err := apiClient.ClusterApi.GetCluster(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		if resp.StatusCode == 404 {
			tflog.Debug(ctx, fmt.Sprintf("Cluster %s does not exist anymore or cannot be found ", clusterId))
			return nil

		}
		return fmt.Errorf("unable to check for cluster %s: %s", clusterId, GetApiErrorDetails(err))
	}

	if clusterResp.GetData().Info.State == openapiclient.CLUSTERSTATE_DELETING {
		tflog.Debug(ctx, fmt.Sprintf("Cluster %s is being deleted ", clusterId))
		return nil
	}

	//First we gather allowList for the cluster
	clusterNalResp, r, err := apiClient.ClusterApi.ListClusterNetworkAllowLists(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		//Cluster could have been deleted
		if r.StatusCode == 404 {
			return nil
		}
		return fmt.Errorf("unable to check network allow list for cluster %s: %s", clusterId, GetApiErrorDetails(err))
	}
	allowList := util.Filter(clusterNalResp.GetData(), func(ep openapiclient.NetworkAllowListData) bool {
		return ep.GetInfo().Id != allowListId
	})

	allowListIds := []string{}
	for _, v := range allowList {
		allowListIds = append(allowListIds, v.GetInfo().Id)

	}

	_, _, err = apiClient.ClusterApi.EditClusterNetworkAllowLists(ctx, accountId, projectId, clusterId).RequestBody(allowListIds).Execute()
	if err != nil {
		return fmt.Errorf("unable to edit network allow list for cluster %s: %s", clusterId, GetApiErrorDetails(err))
	}

	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(2400*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_EDIT_ALLOW_LIST, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}
		} else {
			return retry.RetryableError(errors.New("unable to check edit network allow list for cluster : " + message))
		}
		return retry.RetryableError(errors.New("allow list is being de-associated from the cluster"))
	})

	if err != nil {
		return fmt.Errorf("unable to edit network allow list for cluster %s:%s", clusterId, err)
	}
	return nil

}
