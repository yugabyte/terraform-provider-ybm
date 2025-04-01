/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"errors"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourcePitrCloneType struct{}

func (r resourcePitrCloneType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to clone a namespace in YugabyteDB Aeon.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"project_id": {
				Description: "The ID of the project.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"cluster_id": {
				Description: "The ID of the cluster.",
				Type:        types.StringType,
				Required:    true,
			},
			"clone_namespace_id": {
				Description: "The ID of the namespace clone.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"clone_as": {
				Description: "The name for new cloned namespace.",
				Type:        types.StringType,
				Required:    true,
			},
			"source_namespace_id": {
				Description: "The ID of the namespace to be cloned.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"namespace_name": {
				Description: "The source namespace name to be cloned.",
				Type:        types.StringType,
				Required:    true,
			},
			"namespace_type": {
				Description: "The namespace type.",
				Type:        types.StringType,
				Required:    true,
			},
			"clone_at_millis": {
				Description: "The time in UNIX millis to clone to via PITR Config.",
				Type:        types.Int64Type,
				Optional:    true,
			},
			"state": {
				Description: "The status of the namespace cloning.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
		},
	}, nil
}

func (r resourcePitrCloneType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourcePitrClone{
		p: *(p.(*provider)),
	}, nil
}

type resourcePitrClone struct {
	p provider
}

func getPitrClonePlan(ctx context.Context, plan tfsdk.Plan, pitrClone *PitrClone) diag.Diagnostics {
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &pitrClone.ClusterId)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("namespace_name"), &pitrClone.NamespaceName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("namespace_type"), &pitrClone.NamespaceType)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("clone_at_millis"), &pitrClone.CloneAtMillis)...)

	return diags
}

func getPitrCloneState(ctx context.Context, state tfsdk.State, pitrClone *PitrClone) {
	state.GetAttribute(ctx, path.Root("account_id"), &pitrClone.AccountId)
	state.GetAttribute(ctx, path.Root("project_id"), &pitrClone.ProjectId)
	state.GetAttribute(ctx, path.Root("cluster_id"), &pitrClone.ClusterId)
	state.GetAttribute(ctx, path.Root("clone_namespace_id"), &pitrClone.CloneNamespaceId)
	state.GetAttribute(ctx, path.Root("clone_as"), &pitrClone.CloneAs)
	state.GetAttribute(ctx, path.Root("namespace_name"), &pitrClone.NamespaceName)
	state.GetAttribute(ctx, path.Root("namespace_type"), &pitrClone.NamespaceType)
	state.GetAttribute(ctx, path.Root("source_namespace_id"), &pitrClone.SourceNamespaceId)
	state.GetAttribute(ctx, path.Root("clone_at_millis"), &pitrClone.CloneAtMillis)
	state.GetAttribute(ctx, path.Root("state"), &pitrClone.State)
}

// Create PITR clone
func (r resourcePitrClone) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan PitrClone
	var accountId, message string
	var getAccountOK bool
	resp.Diagnostics.Append(getPitrClonePlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the PITR Clone")
		return
	}

	if (!plan.CloneNamespaceId.Unknown && !plan.CloneNamespaceId.Null) || plan.CloneNamespaceId.Value != "" {
		resp.Diagnostics.AddError(
			"Cloned namespace ID provided for new clone",
			"The clone_namespace_id was provided even though a new clone creation is being requested. Do not include this field in the provider when cloning a namespace.",
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

	clusterId := plan.ClusterId.Value
	namespaceName := plan.NamespaceName.Value
	namespaceType := plan.NamespaceType.Value

	namespacesResp, response, err := apiClient.ClusterApi.GetClusterNamespaces(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to clone namespace", errMsg)
		return
	}

	var namespaceId string
	for _, namespace := range namespacesResp.Data {
		if namespace.GetName() == namespaceName && namespace.GetTableType() == GetNamespaceTypeMap()[namespaceType] {
			namespaceId = namespace.GetId()
		}
	}
	if len(namespaceId) == 0 {
		msg := "No" + namespaceType + "namespace found with name" + namespaceName
		resp.Diagnostics.AddError("Unable to clone namespace:", msg)
	}

	pitrConfigsResp, response, err := apiClient.ClusterApi.ListClusterPitrConfigs(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to clone namespace", errMsg)
		return
	}

	var pitrConfigId string
	for _, pitrConfig := range pitrConfigsResp.GetData() {
		if pitrConfig.Spec.DatabaseId == namespaceId {
			pitrConfigId = *pitrConfig.Info.Id
			break
		}
	}

	cloneSpec := openapiclient.NewDatabaseCloneSpec()

	if len(pitrConfigId) == 0 {
		// No PITR config exists, so we create one and clone to current time.
		// "clone_at_millis" should not be provided in this case.
		if (!plan.CloneAtMillis.Unknown && !plan.CloneAtMillis.Null) || plan.CloneAtMillis.Value != 0 {
			resp.Diagnostics.AddError(
				"Clone time provided for cloning a namespace without a pre exsiting PITR config",
				"The clone_at_millis was provided even though cloning of a namespace that is not assocaited to a PITR config is being requested. Do not include this field in the provider.",
			)
			return
		}
		cloneSpec.SetCloneNow(*openapiclient.NewDatabaseCloneNowSpec(namespaceId, plan.CloneAs.Value))
	} else {
		if (!plan.CloneAtMillis.Unknown && !plan.CloneAtMillis.Null) || plan.CloneAtMillis.Value != 0 {
			cloneSpec.SetClonePointInTime(*openapiclient.NewDatabaseClonePITSpec(plan.CloneAtMillis.Value, pitrConfigId, plan.CloneAs.Value))
		} else {
			cloneSpec.SetCloneNow(*openapiclient.NewDatabaseCloneNowSpec(namespaceId, plan.CloneAs.Value))
		}
	}

	pitrCloneResp, response, err := apiClient.ClusterApi.CloneDatabase(context.Background(), accountId, projectId, clusterId).DatabaseCloneSpec(*cloneSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		resp.Diagnostics.AddError("Unable to clone namespace ", errMsg)
		return
	}

	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_CLONE_DB_PITR, apiClient, ctx)

		tflog.Info(ctx, "Cloning namespace in progress, state: "+asState)

		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("Cloning namespace in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for namespace cloning to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "Cloning namespace failed"
		}
		resp.Diagnostics.AddError("Unable to clone namespace:", msg)
		return
	}

	// Set the computed fields
	plan.AccountId = types.String{Value: accountId}
	plan.ProjectId = types.String{Value: projectId}
	plan.CloneNamespaceId = types.String{Value: pitrCloneResp.Data.Info.Id}
	plan.SourceNamespaceId = types.String{Value: namespaceId}
	plan.State = types.String{Value: *&pitrCloneResp.Data.Info.State}

	diags := resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Read PITR clone
func (r resourcePitrClone) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state PitrClone
	getPitrCloneState(ctx, req.State, &state)

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update PITR clone
func (r resourcePitrClone) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	resp.Diagnostics.AddError("Unable to update PITR clone.", "Updating PITR clones is not supported.")
	return
}

// Delete PITR clone
func (r resourcePitrClone) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	resp.Diagnostics.AddError("Unable to delete PITR clone.", "Deleting PITR clones is not supported.")
	return
}

// Import PITR clone
func (r resourcePitrClone) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Save the import identifier in the id attribute
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
