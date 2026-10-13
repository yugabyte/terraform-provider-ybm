/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceAmpClusterType struct{}

func (r resourceAmpClusterType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	attributes := ampClusterAttributes()
	attributes["region"] = tfsdk.Attribute{
		Description:   "The region to create the cluster in, for example us-east-1. Changing it forces a new cluster.",
		Type:          types.StringType,
		Required:      true,
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.RequiresReplace()},
	}
	attributes["environment_id"] = tfsdk.Attribute{
		Description:   "The ID of the AMP Managed environment hosting the cluster.",
		Type:          types.StringType,
		Computed:      true,
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
	}
	return tfsdk.Schema{
		Description: `The resource to create a shared PostgreSQL cluster in YugabyteDB Aeon's AMP Managed service. ` +
			`The cluster is created in the account's storage pool with one read-write endpoint on its main branch. ` +
			`Add branches and endpoints with ybm_amp_branch and ybm_amp_endpoint.`,
		Attributes: attributes,
	}, nil
}

func (r resourceAmpClusterType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceAmpCluster{
		p: *(p.(*provider)),
	}, nil
}

type resourceAmpCluster struct {
	p provider
}

func ampClusterConfigFromPlan(plan AmpCluster) (ampClusterConfig, error) {
	return newAmpClusterConfig(plan.PgVersion, plan.Engine, plan.Extensions, plan.Credentials,
		plan.Cpu, plan.Memory, plan.ConnectivityMode, plan.AllowedCidrs, plan.BranchPgSettings, plan.EndpointPgSettings)
}

func (r resourceAmpCluster) ModifyPlan(ctx context.Context, req tfsdk.ModifyResourcePlanRequest, resp *tfsdk.ModifyResourcePlanResponse) {
	modifyAmpClusterPlan(ctx, req, resp)
}

func (r resourceAmpCluster) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan AmpCluster
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	accountId, getAccountOK, message := getAccountId(ctx, r.p.client)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}
	config, err := ampClusterConfigFromPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid cluster configuration", err.Error())
		return
	}

	scope := newAmpScope(r.p.client, accountId, "", "")
	clusterName := plan.ClusterName.Value
	if err := scope.apply(ctx, plan.Region.Value, ampIntentCreate, ampCreateClusterItems(scope.pool(), clusterName, config)); err != nil {
		resp.Diagnostics.AddError("Unable to create cluster "+clusterName, err.Error())
		return
	}
	waitErr := waitForAmp(ctx, scope, ampClusterTargets(scope.pool(), clusterName))
	// The cluster exists from here on, so its state is saved even when it did not become ready:
	// Terraform then marks it tainted rather than losing track of it.
	cluster, _, err := resourceAmpClusterRead(ctx, scope, clusterName, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of cluster "+clusterName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cluster)...)
	if waitErr != nil {
		resp.Diagnostics.AddError("Cluster "+clusterName+" did not become ready", waitErr.Error())
	}
}

// listAmpManagedResources returns the account's pool as it stands in every region.
func listAmpManagedResources(ctx context.Context, apiClient *openapiclient.APIClient, accountId string) ([]ampEnvironmentResources, error) {
	data, err := ycpDo(ctx, apiClient, http.MethodGet, accountId, "/clusters", url.Values{}, nil)
	if err != nil {
		return nil, err
	}
	var list struct {
		Environments []ampEnvironmentResources `json:"environments"`
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, fmt.Errorf("unable to parse the cluster list: %v", err)
		}
	}
	return list.Environments, nil
}

// resourceAmpClusterRead reads the cluster from the account's pool. found is false when no region
// declares the cluster any more.
func resourceAmpClusterRead(ctx context.Context, scope ampScope, clusterName string, prior AmpCluster) (cluster AmpCluster, found bool, err error) {
	snap, err := scope.snapshot(ctx)
	if err != nil {
		return cluster, false, err
	}
	env, ok := ampEnvDeclaring(snap, ampKindCluster, ampClusterName(scope.pool(), clusterName))
	if !ok {
		return cluster, false, nil
	}
	status, _, err := readAmpCluster(env.Desired, env.Observed, scope.pool(), clusterName)
	if err != nil {
		return cluster, true, err
	}
	ep := status.Endpoint
	cluster = AmpCluster{
		AccountID:             types.String{Value: scope.accountId},
		ClusterName:           types.String{Value: clusterName},
		Region:                types.String{Value: env.Region},
		PgVersion:             types.Int64{Value: status.Config.PgVersion},
		Engine:                types.String{Value: status.Config.Engine},
		Extensions:            stringsToTf(status.Config.Extensions, prior.Extensions),
		Credentials:           ampCredentialsToTf(ep.Config.Password, prior.Credentials),
		Cpu:                   tfString(ep.Config.Cpu),
		Memory:                tfString(ep.Config.Memory),
		SuspendTimeoutSeconds: types.Int64{Null: true},
		ConnectivityMode:      tfString(ep.Config.ConnectivityMode),
		AllowedCidrs:          stringsToTf(ep.Config.AllowedCidrs, prior.AllowedCidrs),
		BranchPgSettings:      mapToTf(status.Config.BranchPgSettings, prior.BranchPgSettings),
		EndpointPgSettings:    mapToTf(ep.Config.PgSettings, prior.EndpointPgSettings),
		ForceDestroy:          prior.ForceDestroy,
		ClusterID:             tfString(status.ClusterID),
		EnvironmentID:         tfString(env.EnvironmentID),
		State:                 tfString(ep.State),
		Host:                  tfString(ep.Host),
		Port:                  tfString(ep.Port),
		Username:              types.String{Value: ampUsername},
		Database:              types.String{Value: ampDatabase},
		TlsCaCertificate:      tfString(ep.TlsCaCertificate),
	}
	return cluster, true, nil
}

func (r resourceAmpCluster) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state AmpCluster
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	accountId := state.AccountID.Value
	if accountId == "" {
		var ok bool
		var message string
		accountId, ok, message = getAccountId(ctx, r.p.client)
		if !ok {
			resp.Diagnostics.AddError("Unable to get account ID", message)
			return
		}
	}

	cluster, found, err := resourceAmpClusterRead(ctx, newAmpScope(r.p.client, accountId, "", ""), state.ClusterName.Value, state)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of cluster "+state.ClusterName.Value, err.Error())
		return
	}
	if !found {
		tflog.Warn(ctx, "Cluster "+state.ClusterName.Value+" no longer exists; removing it from state")
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cluster)...)
}

func (r resourceAmpCluster) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan, state AmpCluster
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope := newAmpScope(r.p.client, state.AccountID.Value, "", "")
	clusterName := state.ClusterName.Value

	config, err := ampClusterConfigFromPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid cluster configuration", err.Error())
		return
	}
	if err := updateAmpCluster(ctx, scope, state.Region.Value, clusterName, config); err != nil {
		resp.Diagnostics.AddError("Unable to update cluster "+clusterName, err.Error())
		return
	}
	if err := waitForAmp(ctx, scope, ampClusterTargets(scope.pool(), clusterName)); err != nil {
		resp.Diagnostics.AddError("Cluster "+clusterName+" did not converge", err.Error())
		return
	}
	cluster, _, err := resourceAmpClusterRead(ctx, scope, clusterName, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of cluster "+clusterName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cluster)...)
}

func (r resourceAmpCluster) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state AmpCluster
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope := newAmpScope(r.p.client, state.AccountID.Value, "", "")
	clusterName := state.ClusterName.Value
	if err := deleteAmpCluster(ctx, scope, state.Region.Value, clusterName, boolValue(state.ForceDestroy)); err != nil {
		resp.Diagnostics.AddError("Unable to delete cluster "+clusterName, err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

// ImportState imports a cluster by its name.
func (r resourceAmpCluster) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("cluster_name"), req, resp)
}
