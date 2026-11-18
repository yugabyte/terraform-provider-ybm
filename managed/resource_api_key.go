/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *resourceApiKey) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The resource to issue an API Key in YugabyteDB Aeon.`,
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account this user belongs to.",
				Computed:    true,
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project this user belongs to.",
				Computed:    true,
			},
			"api_key_id": schema.StringAttribute{
				Description: "The ID of the API Key. Created automatically when an API Key is created. Use this ID to get a specific API Key.",
				Computed:    true,
				Optional:    true,
			},
			"name": schema.StringAttribute{
				Description: "The name of the API Key.",
				Required:    true,
			},
			"duration": schema.Int64Attribute{
				Description: "The duration for which the API Key will be valid. 0 denotes that the key will never expire.",
				Required:    true,
			},
			"unit": schema.StringAttribute{
				Description: "The time units for which the API Key will be valid. Available options are Hours, Days, and Months.",
				Required:    true,
			},
			"role_name": schema.StringAttribute{
				Description: "The role of the API Key.",
				Required:    true,
			},
			"allow_list_ids": schema.SetAttribute{
				Description: "List of IDs of the allow lists assigned to the API Key.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"description": schema.StringAttribute{
				Description: "The description of the API Key.",
				Computed:    true,
				Optional:    true,
			},
			"api_key": schema.StringAttribute{
				Description: "The API Key.",
				Computed:    true,
				Sensitive:   true,
			},
			"status": schema.StringAttribute{
				Description: "The status of the API Key.",
				Computed:    true,
			},
			"issuer": schema.StringAttribute{
				Description: "The issuer of the API Key.",
				Computed:    true,
			},
			"last_used": schema.StringAttribute{
				Description: "The last used time of the API Key.",
				Computed:    true,
			},
			"expiration": schema.StringAttribute{
				Description: "The expiry time of the API Key.",
				Computed:    true,
			},
			"date_created": schema.StringAttribute{
				Description: "The creation time of the API Key.",
				Computed:    true,
			},
		},
	}
}

func newResourceApiKey() resource.Resource {
	return &resourceApiKey{}
}

func (r *resourceApiKey) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *resourceApiKey) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type resourceApiKey struct {
	p provider
}

func getApiKeyPlan(ctx context.Context, plan tfsdk.Plan, apiKey *ApiKey) diag.Diagnostics {
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("api_key_id"), &apiKey.ApiKeyID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("name"), &apiKey.Name)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("duration"), &apiKey.Duration)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("unit"), &apiKey.Unit)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("role_name"), &apiKey.RoleName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("description"), &apiKey.Description)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("allow_list_ids"), &apiKey.AllowListIDs)...)

	return diags
}

func GetKeyTimeConversionMap() map[string]int {
	return map[string]int{
		"Hours":  1,
		"Days":   24,
		"Months": 24 * 30,
	}
}

// Create API Key
func (r resourceApiKey) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan ApiKey
	var accountId, message string
	var getAccountOK bool
	resp.Diagnostics.Append(getApiKeyPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the API Key")
		return
	}

	if (!plan.ApiKeyID.IsUnknown() && !plan.ApiKeyID.IsNull()) || plan.ApiKeyID.ValueString() != "" {
		resp.Diagnostics.AddError(
			"API Key ID provided for new API Key",
			"The api_key_id was provided even though a new API Key is being issued. Do not include this field in the provider when creating an API Key.",
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

	name := plan.Name.ValueString()

	duration := plan.Duration.ValueInt64()
	unit := plan.Unit.ValueString()
	var expiryHours int
	validTimeUnit := false
	for k, v := range GetKeyTimeConversionMap() {
		if unit == k {
			validTimeUnit = true
			expiryHours = int(duration) * v
		}
	}
	if !validTimeUnit {
		resp.Diagnostics.AddError(
			"Invalid time unit for API Key creation",
			"Available options are: Hours, Days, and Months.",
		)
		return
	}

	roleName := plan.RoleName.ValueString()

	roleResp, response, err := apiClient.RoleApi.ListRbacRoles(ctx, accountId).RoleTypes("ALL").Limit(100).DisplayName(roleName).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create API Key", errMsg)
		return
	}
	if len(roleResp.Data) < 1 {
		resp.Diagnostics.AddError(
			"Unable to create API Key",
			"The role provided for the API Key does not exist.",
		)
		return
	}
	roleId := roleResp.Data[0].Info.Id

	apiKeySpec := openapiclient.NewApiKeySpec(name, int32(expiryHours))
	apiKeySpec.SetRoleId(roleId)

	if plan.Description.ValueString() != "" {
		apiKeySpec.SetDescription(plan.Description.ValueString())
	}

	allowListIDs := []string{}
	if plan.AllowListIDs != nil {
		for i := range plan.AllowListIDs {
			allowListIDs = append(allowListIDs, plan.AllowListIDs[i].ValueString())
		}
	}
	apiKeySpec.SetAllowListInfo(allowListIDs)

	apiKeyResp, response, err := apiClient.AuthApi.CreateApiKey(ctx, accountId).ApiKeySpec(*apiKeySpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to create API Key ", errMsg)
		return
	}
	apiKeyId := apiKeyResp.Data.Info.Id
	key := apiKeyResp.Jwt

	apiKey, readOK, message := resourceApiKeyRead(accountId, projectId, apiKeyId, duration, unit, *key, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the API Key ", message)
		return
	}
	tflog.Debug(ctx, "API Key Create: API Key on read from API server", map[string]interface{}{
		"API Key": apiKey})

	diags := resp.State.Set(ctx, &apiKey)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getIDsFromApiKeyState(ctx context.Context, state tfsdk.State, apiKey *ApiKey) {
	state.GetAttribute(ctx, path.Root("account_id"), &apiKey.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &apiKey.ProjectID)
	state.GetAttribute(ctx, path.Root("name"), &apiKey.Name)
	state.GetAttribute(ctx, path.Root("duration"), &apiKey.Duration)
	state.GetAttribute(ctx, path.Root("unit"), &apiKey.Unit)
	state.GetAttribute(ctx, path.Root("role_name"), &apiKey.RoleName)
	state.GetAttribute(ctx, path.Root("api_key_id"), &apiKey.ApiKeyID)
	state.GetAttribute(ctx, path.Root("api_key"), &apiKey.ApiKey)
}

func resourceApiKeyRead(accountId string, projectId string, apiKeyId string, duration int64, unit string, key string, apiClient *openapiclient.APIClient) (apiKey ApiKey, readOK bool, errorMessage string) {
	apiKeyResp, response, err := apiClient.AuthApi.GetApiKey(context.Background(), accountId, apiKeyId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return apiKey, false, errMsg
	}

	apiKey.AccountID = types.StringValue(accountId)
	apiKey.ProjectID = types.StringValue(projectId)
	apiKey.ApiKeyID = types.StringValue(apiKeyId)
	apiKey.Name = types.StringValue(apiKeyResp.Data.Spec.Name)
	apiKey.Duration = types.Int64Value(duration)
	apiKey.Unit = types.StringValue(unit)
	apiKey.ApiKey = types.StringValue(key)
	apiKey.Description = types.StringValue(apiKeyResp.Data.Spec.GetDescription())
	var allowListIDs []types.String
	for _, elem := range *apiKeyResp.Data.Spec.AllowListInfo {
		allowListIDs = append(allowListIDs, types.StringValue(elem))
	}
	apiKey.AllowListIDs = allowListIDs
	apiKey.RoleName = types.StringValue(apiKeyResp.Data.Info.Role.Info.GetDisplayName())
	apiKey.Status = types.StringValue(string(apiKeyResp.Data.Info.Status))
	apiKey.Issuer = types.StringValue(apiKeyResp.Data.Info.Issuer)
	apiKey.LastUsed = types.StringValue(apiKeyResp.Data.Info.GetLastUsedTime())
	apiKey.ExpiryTime = types.StringValue(apiKeyResp.Data.Info.ExpiryTime)
	apiKey.CreatedAt = types.StringValue(apiKeyResp.Data.Info.Metadata.Get().GetCreatedOn())

	return apiKey, true, ""
}

// Read API Key
func (r resourceApiKey) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ApiKey
	getIDsFromApiKeyState(ctx, req.State, &state)

	tflog.Debug(ctx, "API Key Read: API Key name and role from state", map[string]interface{}{
		"Name":     state.Name,
		"RoleName": state.RoleName})

	apiKey, readOK, message := resourceApiKeyRead(state.AccountID.ValueString(), state.ProjectID.ValueString(), state.ApiKeyID.ValueString(), state.Duration.ValueInt64(), state.Unit.ValueString(), state.ApiKey.ValueString(), r.p.client)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the API Key ", message)
		return
	}
	tflog.Debug(ctx, "API Key Read: API Key on read from API server", map[string]interface{}{
		"API Key": apiKey})

	diags := resp.State.Set(ctx, &apiKey)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update API Key
func (r resourceApiKey) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unable to update API Key", "Updating API Keys is not supported. Delete and recreate the provider.")
	return
}

// Revoke API Key
func (r resourceApiKey) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ApiKey
	getIDsFromApiKeyState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	apiKeyId := state.ApiKeyID.ValueString()

	apiClient := r.p.client

	response, err := apiClient.AuthApi.RevokeApiKey(context.Background(), accountId, apiKeyId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to revoke the API Key ", errMsg)
		return
	}

	resp.State.RemoveResource(ctx)

}

// Import API Key
func (r resourceApiKey) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Save the import identifier in the id attribute
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
