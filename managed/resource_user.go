/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *resourceUser) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The resource to invite a user in YugabyteDB Aeon.`,
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account this user belongs to.",
				Computed:    true,
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project this user belongs to.",
				Computed:    true,
			},
			"user_id": schema.StringAttribute{
				Description: "The ID of the user. Created automatically when a user is invited. Use this ID to get a specific user.",
				Computed:    true,
				Optional:    true,
			},
			"email": schema.StringAttribute{
				Description: "The email of the user.",
				Required:    true,
			},
			"role_name": schema.StringAttribute{
				Description: "The role of the user.",
				Required:    true,
			},
			"user_name": schema.StringAttribute{
				Description: "The name of the user.",
				Computed:    true,
			},
			"user_state": schema.StringAttribute{
				Description: "The state of the user.",
				Computed:    true,
			},
		},
	}
}

func newResourceUser() resource.Resource {
	return &resourceUser{}
}

func (r *resourceUser) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *resourceUser) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type resourceUser struct {
	p provider
}

func getUserPlan(ctx context.Context, plan tfsdk.Plan, user *User) diag.Diagnostics {
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("user_id"), &user.UserID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("email"), &user.Email)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("role_name"), &user.RoleName)...)

	return diags
}

// Invite user
func (r resourceUser) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan User
	var accountId, message string
	var getAccountOK bool
	resp.Diagnostics.Append(getUserPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the user")
		return
	}

	if (!plan.UserID.IsUnknown() && !plan.UserID.IsNull()) || plan.UserID.ValueString() != "" {
		resp.Diagnostics.AddError(
			"User ID provided for new user",
			"The user_id was provided even though a new user is being invited. Do not include this field in the provider when inviting a user.",
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

	email := plan.Email.ValueString()
	roleName := plan.RoleName.ValueString()

	roleResp, response, err := apiClient.RoleApi.ListRbacRoles(ctx, accountId).RoleTypes("ALL").Limit(100).DisplayName(roleName).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to invite user", errMsg)
		return
	}
	if len(roleResp.Data) < 1 || roleResp.Data[0].Info.GetDisplayName() != roleName {
		resp.Diagnostics.AddError(
			"Unable to invite user",
			fmt.Sprintf("The role '%s' does not exist.", roleName),
		)
		return
	}
	roleId := roleResp.Data[0].Info.Id

	users := []openapiclient.InviteUserSpec{}
	user := *openapiclient.NewInviteUserSpecWithDefaults()
	user.SetEmail(email)

	user.SetRoleId(roleId)

	users = append(users, user)

	usersSpec := *openapiclient.NewBatchInviteUserSpecWithDefaults()
	usersSpec.SetUserList(users)

	userResp, response, err := apiClient.AccountApi.BatchInviteAccountUsers(ctx, accountId).BatchInviteUserSpec(usersSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to invite user ", errMsg)
		return
	}
	if !userResp.Data.GetUserList()[0].GetIsSuccessful() {
		resp.Diagnostics.AddError("Unable to invite user ", userResp.Data.GetUserList()[0].GetErrorMessage())
		return
	}
	userId := userResp.Data.GetUserList()[0].GetInviteUserData().Info.Id

	createdUser, readOK, message := resourceUserRead(accountId, projectId, email, userId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the user ", message)
		return
	}
	tflog.Debug(ctx, "User Create: User on read from API server", map[string]interface{}{
		"User": createdUser})

	diags := resp.State.Set(ctx, &createdUser)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getIDsFromUserState(ctx context.Context, state tfsdk.State, user *User) {
	state.GetAttribute(ctx, path.Root("account_id"), &user.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &user.ProjectID)
	state.GetAttribute(ctx, path.Root("email"), &user.Email)
	state.GetAttribute(ctx, path.Root("role_name"), &user.RoleName)
	state.GetAttribute(ctx, path.Root("user_id"), &user.UserID)
}

func resourceUserRead(accountId string, projectId string, email string, userId string, apiClient *openapiclient.APIClient) (user User, readOK bool, errorMessage string) {
	userResp, response, err := apiClient.AccountApi.ListAccountUsers(context.Background(), accountId).Email(email).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return user, false, errMsg
	}

	user.AccountID = types.StringValue(accountId)
	user.ProjectID = types.StringValue(projectId)
	user.UserID = types.StringValue(userId)
	user.Email = types.StringValue(email)
	user.RoleName = types.StringValue(userResp.Data[0].GetRoleInfo()[0].GetDisplayName())
	user.UserName = types.StringValue(userResp.Data[0].Spec.GetFirstName() + userResp.Data[0].Spec.GetLastName())
	user.UserState = types.StringValue(string(userResp.Data[0].Info.State))

	return user, true, ""
}

// Read user
func (r resourceUser) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state User
	getIDsFromUserState(ctx, req.State, &state)

	tflog.Debug(ctx, "User Read: User email and role from state", map[string]interface{}{
		"Email":    state.Email,
		"RoleName": state.RoleName})

	user, readOK, message := resourceUserRead(state.AccountID.ValueString(), state.ProjectID.ValueString(), state.Email.ValueString(), state.UserID.ValueString(), r.p.client)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the user ", message)
		return
	}
	tflog.Debug(ctx, "User Read: User on read from API server", map[string]interface{}{
		"User": user})

	diags := resp.State.Set(ctx, &user)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update user
func (r resourceUser) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan User
	resp.Diagnostics.Append(getUserPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the user")
		return
	}

	apiClient := r.p.client
	var state User
	getIDsFromUserState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	projectId := state.ProjectID.ValueString()
	userId := state.UserID.ValueString()
	email := state.Email.ValueString()

	if plan.Email.ValueString() != email {
		resp.Diagnostics.AddError(
			"User Email modified for existing user",
			"The email was modified for an existing user. Do not modify this field in the provider when updating an existing user. Only the user role can be modified.",
		)
		return
	}

	roleName := plan.RoleName.ValueString()

	roleResp, response, err := apiClient.RoleApi.ListRbacRoles(ctx, accountId).RoleTypes("ALL").Limit(100).DisplayName(roleName).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to modify user role", errMsg)
		return
	}
	if len(roleResp.Data) < 1 || roleResp.Data[0].Info.GetDisplayName() != roleName {
		resp.Diagnostics.AddError(
			"Unable to modify user role",
			fmt.Sprintf("The role '%s' does not exist.", roleName),
		)
		return
	}
	roleId := roleResp.Data[0].Info.Id

	modifyUserRoleRequest := *openapiclient.NewModifyUserRoleRequest(roleId)
	response, err = apiClient.AccountApi.ModifyUserRole(ctx, accountId, userId).ModifyUserRoleRequest(modifyUserRoleRequest).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to modify user role ", errMsg)
		return
	}

	updatedUser, readOK, message := resourceUserRead(accountId, projectId, email, userId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the user ", message)
		return
	}
	tflog.Debug(ctx, "User Update: User on read from API server", map[string]interface{}{
		"User": updatedUser})

	diags := resp.State.Set(ctx, &updatedUser)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete user
func (r resourceUser) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state User
	getIDsFromUserState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	userId := state.UserID.ValueString()

	apiClient := r.p.client

	response, err := apiClient.AccountApi.RemoveAccountUser(context.Background(), accountId, userId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to delete the user ", errMsg)
		return
	}

	resp.State.RemoveResource(ctx)

}

// Import user
func (r resourceUser) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Save the import identifier in the id attribute
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
