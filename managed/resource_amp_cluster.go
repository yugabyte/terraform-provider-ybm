/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
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
			`The cluster is created in the account's storage pool with one read-write endpoint.`,
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
	return newAmpClusterConfig(plan.PgVersion, plan.Engine, plan.Extensions, plan.Password,
		plan.Cpu, plan.Memory, plan.ConnectivityMode, plan.AllowedCidrs)
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
	apiClient := r.p.client

	accountId, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}
	config, err := ampClusterConfigFromPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid cluster configuration", err.Error())
		return
	}

	clusterName := plan.ClusterName.Value
	region := plan.Region.Value
	// The account's storage pool is the account ID; YBM refuses names outside it.
	patch := ampManagedPatch{Regions: []ampRegionPatch{{
		Region: region,
		Resources: []ampPatchItem{
			{Kind: ampKindCluster, Name: ampClusterName(accountId, clusterName), Spec: config.clusterSpec()},
			{Kind: ampKindEndpoint, Name: ampEndpointName(accountId, clusterName), Spec: config.endpointSpec(true)},
		},
	}}}
	if _, err := ycpDo(ctx, apiClient, http.MethodPost, accountId, "/clusters", nil, patch); err != nil {
		resp.Diagnostics.AddError("Unable to create cluster "+clusterName, err.Error())
		return
	}

	cluster, err := waitForAmpCluster(ctx, apiClient, accountId, clusterName, plan)
	if err != nil {
		resp.Diagnostics.AddError("Cluster "+clusterName+" did not become ready", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cluster)...)
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
func resourceAmpClusterRead(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, clusterName string, prior AmpCluster) (cluster AmpCluster, found bool, err error) {
	envs, err := listAmpManagedResources(ctx, apiClient, accountId)
	if err != nil {
		return cluster, false, err
	}
	for _, env := range envs {
		status, ok, err := readAmpCluster(env.Desired, env.Observed, accountId, clusterName)
		if err != nil {
			return cluster, true, err
		}
		if !ok {
			continue
		}
		cluster = AmpCluster{
			AccountID:        types.String{Value: accountId},
			ClusterName:      types.String{Value: clusterName},
			Region:           types.String{Value: env.Region},
			PgVersion:        types.Int64{Value: status.Config.PgVersion},
			Engine:           types.String{Value: status.Config.Engine},
			Extensions:       stringsToTf(status.Config.Extensions),
			Password:         tfString(status.Config.Password),
			Cpu:              tfString(status.Config.Cpu),
			Memory:           tfString(status.Config.Memory),
			ConnectivityMode: tfString(status.Config.ConnectivityMode),
			AllowedCidrs:     stringsToTf(status.Config.AllowedCidrs),
			ClusterID:        tfString(status.ClusterID),
			EnvironmentID:    tfString(env.EnvironmentID),
			State:            tfString(status.State),
			Host:             tfString(status.Host),
			Port:             tfString(status.Port),
			Username:         types.String{Value: ampUsername},
			Database:         types.String{Value: ampDatabase},
			TlsCaCertificate: tfString(status.TlsCaCertificate),
		}
		// The server echoes the password today; keep the configured one if it ever stops.
		if cluster.Password.Null && !prior.Password.Null {
			cluster.Password = prior.Password
		}
		return cluster, true, nil
	}
	return cluster, false, nil
}

// waitForAmpCluster polls until the cluster's read-write endpoint has converged on its desired
// spec, then returns the cluster as read.
func waitForAmpCluster(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, clusterName string, prior AmpCluster) (AmpCluster, error) {
	var cluster AmpCluster
	var terminal error
	retryPolicy := retry.WithMaxDuration(ampOperationTimeout, retry.NewConstant(ampPollInterval))
	err := retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		envs, err := listAmpManagedResources(ctx, apiClient, accountId)
		if err != nil {
			return retry.RetryableError(err)
		}
		for _, env := range envs {
			if _, ok := findAmpDesired(env.Desired, ampKindCluster, ampClusterName(accountId, clusterName)); !ok {
				continue
			}
			done, err := ampEndpointProgress(env.Desired, env.Observed, accountId, clusterName)
			if err != nil {
				terminal = err
				return err
			}
			if done {
				return nil
			}
			tflog.Debug(ctx, "Cluster "+clusterName+" has not converged yet")
			return retry.RetryableError(errors.New("the cluster is not ready yet"))
		}
		terminal = fmt.Errorf("cluster %s is not declared in any region", clusterName)
		return terminal
	})
	if terminal != nil {
		return cluster, terminal
	}
	if err != nil {
		return cluster, fmt.Errorf("timed out after %v waiting for the cluster to become ready", ampOperationTimeout)
	}
	cluster, _, err = resourceAmpClusterRead(ctx, apiClient, accountId, clusterName, prior)
	return cluster, err
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

	cluster, found, err := resourceAmpClusterRead(ctx, r.p.client, accountId, state.ClusterName.Value, state)
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
	apiClient := r.p.client
	accountId := state.AccountID.Value
	clusterName := state.ClusterName.Value

	config, err := ampClusterConfigFromPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid cluster configuration", err.Error())
		return
	}
	// Only the endpoint changes in place: every cluster-level attribute forces a replacement.
	patch := ampManagedPatch{Regions: []ampRegionPatch{{
		Region: state.Region.Value,
		Resources: []ampPatchItem{
			{Kind: ampKindEndpoint, Name: ampEndpointName(accountId, clusterName), Spec: config.endpointSpec(false)},
		},
	}}}
	if _, err := ycpDo(ctx, apiClient, http.MethodPut, accountId, "/clusters", nil, patch); err != nil {
		resp.Diagnostics.AddError("Unable to update cluster "+clusterName, err.Error())
		return
	}

	cluster, err := waitForAmpCluster(ctx, apiClient, accountId, clusterName, plan)
	if err != nil {
		resp.Diagnostics.AddError("Cluster "+clusterName+" did not converge", err.Error())
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
	apiClient := r.p.client
	accountId := state.AccountID.Value
	clusterName := state.ClusterName.Value

	// Deleting the cluster cascades to its branches and endpoints.
	patch := ampManagedPatch{Regions: []ampRegionPatch{{
		Region:    state.Region.Value,
		Resources: []ampPatchItem{{Kind: ampKindCluster, Name: ampClusterName(accountId, clusterName)}},
	}}}
	if _, err := ycpDo(ctx, apiClient, http.MethodPatch, accountId, "/clusters", nil, patch); err != nil && !isYcpNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete cluster "+clusterName, err.Error())
		return
	}

	retryPolicy := retry.WithMaxDuration(ampOperationTimeout, retry.NewConstant(ampPollInterval))
	err := retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		envs, err := listAmpManagedResources(ctx, apiClient, accountId)
		if err != nil {
			return retry.RetryableError(err)
		}
		for _, env := range envs {
			if _, ok := findAmpObserved(env.Observed, ampKindCluster, ampClusterName(accountId, clusterName)); ok {
				return retry.RetryableError(errors.New("the cluster is still being deleted"))
			}
		}
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete cluster "+clusterName, "The operation timed out waiting for the cluster deletion to complete.")
		return
	}
	resp.State.RemoveResource(ctx)
}

// ImportState imports a cluster by its name.
func (r resourceAmpCluster) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("cluster_name"), req, resp)
}
