/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceAllowListType struct{}

func (r resourceAllowListType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create an allow list in YugabyteDB Managed.`,
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
				Description: "The ID of the allow list. Created automatically when an allow list is created. Use this ID to get a specific allow list.",
				Type:        types.StringType,
				Computed:    true,
				Optional:    true,
			},
			"allow_list_name": {
				Description: "The name of the allow list.",
				Type:        types.StringType,
				Required:    true,
			},
			"allow_list_description": {
				Description: "The description of the allow list.",
				Type:        types.StringType,
				Required:    true,
			},
			"cidr_list": {
				Description: "The CIDR list of the allow list.",
				Type: types.SetType{
					ElemType: types.StringType,
				},
				Required: true,
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
	diags := req.Plan.Get(ctx, &plan)
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

	networkAllowListSpec := *openapiclient.NewNetworkAllowListSpec(allowListName, allowListDesc, cidrList) // NetworkAllowListSpec | Allow list specification (optional)

	allowListResp, response, err := apiClient.NetworkApi.CreateNetworkAllowList(context.Background(), accountId, projectId).NetworkAllowListSpec(networkAllowListSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create allow list ", errMsg)
		return
	}
	allowListId := allowListResp.Data.Info.Id

	allowList, readOK, message := resourceAllowListRead(accountId, projectId, allowListId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the allow list ", message)
		return
	}
	tflog.Debug(ctx, "Allow List Create: Allow list on read from API server", map[string]interface{}{
		"Allow List": allowList})

	diags = resp.State.Set(ctx, &allowList)
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
}

func resourceAllowListRead(accountId string, projectId string, allowListId string, apiClient *openapiclient.APIClient) (allowList AllowList, readOK bool, errorMessage string) {
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

	cidrList := []string{}
	for i := range state.CIDRList {
		cidrList = append(cidrList, state.CIDRList[i].Value)
	}

	tflog.Debug(ctx, "Allow List Read: CIDR List from state", map[string]interface{}{
		"CIDR List": state.CIDRList})

	allowList, readOK, message := resourceAllowListRead(state.AccountID.Value, state.ProjectID.Value, state.AllowListID.Value, r.p.client)
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

	apiClient := r.p.client

	response, err := apiClient.NetworkApi.DeleteNetworkAllowList(context.Background(), accountId, projectId, allowListId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to delete the allow list ", errMsg)
		return
	}

	resp.State.RemoveResource(ctx)

}

// Import allow list
func (r resourceAllowList) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
