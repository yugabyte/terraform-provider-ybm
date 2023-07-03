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

type resourceApiKeyType struct{}

func (r resourceApiKeyType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to issue an API Key in YugabyteDB Managed.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this user belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this user belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"api_key_id": {
				Description: "The ID of the API Key. Created automatically when an API Key is created. Use this ID to get a specific API Key.",
				Type:        types.StringType,
				Computed:    true,
				Optional:    true,
			},
			"name": {
				Description: "The name of the API Key.",
				Type:        types.StringType,
				Required:    true,
			},
			"duration": {
				Description: "The duration for which the API Key will be valid. 0 denotes that the key will never expire.",
				Type:        types.Int64Type,
				Required:    true,
			},
			"unit": {
				Description: "The time units for which the API Key will be valid. Available options are Hours, Days, and Months.",
				Type:        types.StringType,
				Required:    true,
			},
			"role_name": {
				Description: "The role of the API Key.",
				Type:        types.StringType,
				Required:    true,
			},
			"description": {
				Description: "The description of the API Key.",
				Type:        types.StringType,
				Computed:    true,
				Optional:    true,
			},
			"api_key": {
				Description: "The API Key.",
				Type:        types.StringType,
				Computed:    true,
				Sensitive:   true,
			},
			"status": {
				Description: "The status of the API Key.",
				Type:        types.StringType,
				Computed:    true,
			},
			"issuer": {
				Description: "The issuer of the API Key.",
				Type:        types.StringType,
				Computed:    true,
			},
			"last_used": {
				Description: "The last used time of the API Key.",
				Type:        types.StringType,
				Computed:    true,
			},
			"expiration": {
				Description: "The expiry time of the API Key.",
				Type:        types.StringType,
				Computed:    true,
			},
			"date_created": {
				Description: "The creation time of the API Key.",
				Type:        types.StringType,
				Computed:    true,
			},
		},
	}, nil
}

func (r resourceApiKeyType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceApiKey{
		p: *(p.(*provider)),
	}, nil
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
func (r resourceApiKey) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

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

	if (!plan.ApiKeyID.Unknown && !plan.ApiKeyID.Null) || plan.ApiKeyID.Value != "" {
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

	name := plan.Name.Value

	duration := plan.Duration.Value
	unit := plan.Unit.Value
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
			"Available options are Hours, Days, and Months.",
		)
	}

	roleName := plan.RoleName.Value

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

	if plan.Description.Value != "" {
		apiKeySpec.SetDescription(plan.Description.Value)
	}

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

	apiKey.AccountID.Value = accountId
	apiKey.ProjectID.Value = projectId
	apiKey.ApiKeyID.Value = apiKeyId
	apiKey.Name.Value = apiKeyResp.Data.Spec.Name
	apiKey.Duration.Value = duration
	apiKey.Unit.Value = unit
	apiKey.ApiKey.Value = key
	description := apiKeyResp.Data.Spec.GetDescription()
	if description != "" {
		apiKey.Description.Value = description
	}
	apiKey.RoleName.Value = apiKeyResp.Data.Info.Role.Info.GetDisplayName()
	apiKey.Status.Value = string(apiKeyResp.Data.Info.Status)
	apiKey.Issuer.Value = apiKeyResp.Data.Info.Issuer
	apiKey.LastUsed.Value = apiKeyResp.Data.Info.GetLastUsedTime()
	apiKey.ExpiryTime.Value = apiKeyResp.Data.Info.ExpiryTime
	apiKey.CreatedAt.Value = apiKeyResp.Data.Info.Metadata.Get().GetCreatedOn()

	return apiKey, true, ""
}

// Read API Key
func (r resourceApiKey) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state ApiKey
	getIDsFromApiKeyState(ctx, req.State, &state)

	tflog.Debug(ctx, "API Key Read: API Key name and role from state", map[string]interface{}{
		"Name":     state.Name,
		"RoleName": state.RoleName})

	apiKey, readOK, message := resourceApiKeyRead(state.AccountID.Value, state.ProjectID.Value, state.ApiKeyID.Value, state.Duration.Value, state.Unit.Value, state.ApiKey.Value, r.p.client)
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
func (r resourceApiKey) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	resp.Diagnostics.AddError("Unable to update API Key", "Updating API Keys is not currently supported. Delete and recreate the provider.")
	return
}

// Revoke API Key
func (r resourceApiKey) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state ApiKey
	getIDsFromApiKeyState(ctx, req.State, &state)
	accountId := state.AccountID.Value
	apiKeyId := state.ApiKeyID.Value

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
func (r resourceApiKey) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
