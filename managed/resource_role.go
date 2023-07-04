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

type resourceRoleType struct{}

func (r resourceRoleType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create a custom role in YugabyteDB Managed.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this role belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this role belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"role_id": {
				Description: "The ID of the role. Created automatically when a role is created. Use this ID to get a specific role.",
				Type:        types.StringType,
				Computed:    true,
				Optional:    true,
			},
			"role_name": {
				Description: "The name of the role.",
				Type:        types.StringType,
				Required:    true,
			},
			"role_description": {
				Description: "The description of the role.",
				Type:        types.StringType,
				Computed:    true,
				Optional:    true,
			},
			"permission_list": {
				Description: "The permission list of the role.",
				Required:    true,
				Attributes: tfsdk.SetNestedAttributes(map[string]tfsdk.Attribute{
					"resource_type": {
						Type:     types.StringType,
						Required: true,
					},
					"operation_groups": {
						Type: types.SetType{
							ElemType: types.StringType,
						},
						Required: true,
					},
				}),
			},
			"effective_permission_list": {
				Description: "The effective permission list of the role.",
				Computed:    true,
				Attributes: tfsdk.SetNestedAttributes(map[string]tfsdk.Attribute{
					"resource_type": {
						Type:     types.StringType,
						Computed: true,
					},
					"operation_groups": {
						Type: types.SetType{
							ElemType: types.StringType,
						},
						Computed: true,
					},
				}),
			},
			"users": {
				Description: "List of the emails of the users the role is assigned to.",
				Type: types.SetType{
					ElemType: types.StringType,
				},
				Computed: true,
			},
			"api_keys": {
				Description: "List of the API keys the role is assigned to.",
				Type: types.SetType{
					ElemType: types.StringType,
				},
				Computed: true,
			},
		},
	}, nil
}

func (r resourceRoleType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceRole{
		p: *(p.(*provider)),
	}, nil
}

type resourceRole struct {
	p provider
}

func createRoleSpec(apiClient *openapiclient.APIClient, plan Role) (roleSpec *openapiclient.RoleSpec) {

	rolePermissions := []openapiclient.ResourcePermissionInfo{}
	for _, permission := range plan.PermissionList {
		operationGroups := []openapiclient.ResourceOperationGroup{}
		for i := range permission.OperationGroups {
			operationGroups = append(operationGroups, *openapiclient.NewResourceOperationGroup(openapiclient.ResourceOperationGroupEnum(permission.OperationGroups[i].Value)))
		}
		rolePermissions = append(rolePermissions, *openapiclient.NewResourcePermissionInfo(openapiclient.ResourceTypeEnum(permission.ResourceType.Value), operationGroups))
	}

	roleSpec = openapiclient.NewRoleSpec(
		plan.RoleName.Value,
		rolePermissions)
	if plan.RoleDescription.Value != "" {
		roleSpec.SetDescription(plan.RoleDescription.Value)
	}

	return roleSpec
}

func getRolePlan(ctx context.Context, plan tfsdk.Plan, role *Role) diag.Diagnostics {
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("role_id"), &role.RoleID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("role_description"), &role.RoleDescription)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("role_name"), &role.RoleName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("permission_list"), &role.PermissionList)...)

	return diags
}

// Create role
func (r resourceRole) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan Role
	var accountId, message string
	var getAccountOK bool
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(getRolePlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the role")
		return
	}

	if (!plan.RoleID.Unknown && !plan.RoleID.Null) || plan.RoleID.Value != "" {
		resp.Diagnostics.AddError(
			"Role ID provided for new role",
			"The role_id was provided even though a new role is being created. Do not include this field in the provider when creating a role.",
		)
		return
	}

	apiClient := r.p.client

	accountId, getAccountOK, message = getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID ", message)
		return
	}

	roleSpec := createRoleSpec(apiClient, plan)

	roleResp, response, err := apiClient.RoleApi.CreateRole(context.Background(), accountId).RoleSpec(*roleSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create role ", errMsg)
		return
	}
	roleId := roleResp.Data.Info.Id

	role, readOK, message := resourceRoleRead(accountId, projectId, roleId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the role ", message)
		return
	}
	tflog.Debug(ctx, "Role Create: Role on read from API server", map[string]interface{}{
		"Role": role})

	diags = resp.State.Set(ctx, &role)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getIDsFromRoleState(ctx context.Context, state tfsdk.State, role *Role) {
	state.GetAttribute(ctx, path.Root("account_id"), &role.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &role.ProjectID)
	state.GetAttribute(ctx, path.Root("permission_list"), &role.PermissionList)
	state.GetAttribute(ctx, path.Root("effective_permission_list"), &role.EffectivePermissionList)
	state.GetAttribute(ctx, path.Root("role_id"), &role.RoleID)
}

func resourceRoleRead(accountId string, projectId string, roleId string, apiClient *openapiclient.APIClient) (role Role, readOK bool, errorMessage string) {
	roleResp, response, err := apiClient.RoleApi.GetRole(context.Background(), accountId, roleId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return role, false, errMsg
	}

	role.AccountID.Value = accountId
	role.ProjectID.Value = projectId
	role.RoleID.Value = roleId

	role.RoleName.Value = roleResp.Data.Info.GetDisplayName()
	description := roleResp.Data.Spec.GetDescription()
	if description != "" {
		role.RoleDescription.Value = description
	}

	respPermissions := roleResp.Data.Spec.Permissions
	permissionInfo := make([]PermissionInfo, len(respPermissions))
	for index, permission := range respPermissions {
		resourceType := permission.ResourceType
		operationGroups := []types.String{}
		for _, op := range permission.OperationGroups {
			operationGroups = append(operationGroups, types.String{Value: string(op.OperationGroup)})
		}
		permissionInfo[index] = PermissionInfo{
			ResourceType:    types.String{Value: string(resourceType)},
			OperationGroups: operationGroups,
		}
	}
	role.PermissionList = permissionInfo

	respEffectivePermissions := roleResp.Data.Info.GetEffectivePermissions()
	effectivePermissionInfo := make([]PermissionInfo, len(respEffectivePermissions))
	for index, permission := range respEffectivePermissions {
		resourceType := permission.ResourceType
		operationGroups := []types.String{}
		for _, op := range permission.OperationGroups {
			operationGroups = append(operationGroups, types.String{Value: string(op.OperationGroup)})
		}
		effectivePermissionInfo[index] = PermissionInfo{
			ResourceType:    types.String{Value: string(resourceType)},
			OperationGroups: operationGroups,
		}
	}
	role.EffectivePermissionList = effectivePermissionInfo

	users := []types.String{}
	for _, elem := range roleResp.Data.Info.Users {
		users = append(users, types.String{Value: elem.GetEmail()})
	}
	role.Users = users

	apiKeys := []types.String{}
	for _, elem := range roleResp.Data.Info.ApiKeys {
		apiKeys = append(apiKeys, types.String{Value: elem.GetName()})
	}
	role.ApiKeys = apiKeys

	return role, true, ""
}

// Read role
func (r resourceRole) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state Role
	getIDsFromRoleState(ctx, req.State, &state)

	rolePermissions := []openapiclient.ResourcePermissionInfo{}
	for _, permission := range state.PermissionList {
		operationGroups := []openapiclient.ResourceOperationGroup{}
		for i := range permission.OperationGroups {
			operationGroups = append(operationGroups, *openapiclient.NewResourceOperationGroup(openapiclient.ResourceOperationGroupEnum(permission.OperationGroups[i].Value)))
		}
		rolePermissions = append(rolePermissions, *openapiclient.NewResourcePermissionInfo(openapiclient.ResourceTypeEnum(permission.ResourceType.Value), operationGroups))
	}

	tflog.Debug(ctx, "Role Read: Permission List from state", map[string]interface{}{
		"Permission List": state.PermissionList})

	role, readOK, message := resourceRoleRead(state.AccountID.Value, state.ProjectID.Value, state.RoleID.Value, r.p.client)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the role ", message)
		return
	}
	tflog.Debug(ctx, "Role Read: Role on read from API server", map[string]interface{}{
		"Role": role})

	diags := resp.State.Set(ctx, &role)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update role
func (r resourceRole) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {

	var plan Role
	resp.Diagnostics.Append(getRolePlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the role")
		return
	}

	apiClient := r.p.client
	var state Role
	getIDsFromRoleState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	roleId := state.RoleID.Value

	roleSpec := createRoleSpec(apiClient, plan)

	roleResp, response, err := apiClient.RoleApi.UpdateRole(context.Background(), accountId, roleId).RoleSpec(*roleSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to update role ", errMsg)
		return
	}
	roleId = roleResp.Data.Info.Id

	role, readOK, message := resourceRoleRead(accountId, projectId, roleId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the role ", message)
		return
	}
	tflog.Debug(ctx, "Role Update: Role on read from API server", map[string]interface{}{
		"Role": role})

	diags := resp.State.Set(ctx, &role)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}

// Delete role
func (r resourceRole) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state Role
	getIDsFromRoleState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	roleId := state.RoleID.Value

	apiClient := r.p.client

	response, err := apiClient.RoleApi.DeleteRole(context.Background(), accountId, roleId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to delete the role ", errMsg)
		return
	}

	resp.State.RemoveResource(ctx)
}

// Import role
func (r resourceRole) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
