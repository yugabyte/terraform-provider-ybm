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
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

// Environment states in which the agent will never act on desired state.
var byocInactiveStates = map[string]string{
	"PENDING":      "its agent has not enrolled yet; install the agent with the environment's install_hint and apply again",
	"SUSPENDED":    "it is suspended",
	"DRAINING":     "it is draining",
	"DEREGISTERED": "it is deregistered",
}

type resourceByocClusterType struct{}

func (r resourceByocClusterType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	attributes := ampClusterAttributes()
	attributes["environment_id"] = tfsdk.Attribute{
		Description:   "The ID of the BYOC environment to create the cluster in. Changing it forces a new cluster.",
		Type:          types.StringType,
		Required:      true,
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.RequiresReplace()},
	}
	return tfsdk.Schema{
		Description: `The resource to create a PostgreSQL cluster in a BYOC environment registered with ybm_byoc_environment. ` +
			`The cluster is created with one read-write endpoint on its main branch once the environment's agent picks it up. ` +
			`Add branches and endpoints with ybm_amp_branch and ybm_amp_endpoint.`,
		Attributes: attributes,
	}, nil
}

func (r resourceByocClusterType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceByocCluster{
		p: *(p.(*provider)),
	}, nil
}

type resourceByocCluster struct {
	p provider
}

func byocDesiredPath(environmentId string) string {
	return "/environments/" + url.PathEscape(environmentId) + "/desired"
}

func getByocDesired(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, environmentId string) ([]ampResource, error) {
	data, err := ycpDo(ctx, apiClient, http.MethodGet, accountId, byocDesiredPath(environmentId), nil, nil)
	if err != nil {
		return nil, err
	}
	var desired struct {
		Resources []ampResource `json:"resources"`
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &desired); err != nil {
			return nil, fmt.Errorf("unable to parse the desired state of environment %s: %v", environmentId, err)
		}
	}
	return desired.Resources, nil
}

// setByocDesired replaces the environment's whole desired state.
func setByocDesired(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, environmentId string, resources []ampResource) error {
	if resources == nil {
		resources = []ampResource{}
	}
	body := map[string]interface{}{"resources": resources}
	if len(resources) == 0 {
		body["allow_empty"] = true
	}
	_, err := ycpDo(ctx, apiClient, http.MethodPost, accountId, byocDesiredPath(environmentId), nil, body)
	return err
}

// updateByocDesired applies change to the environment's desired state. Desired state is replaced
// as a whole, so the read and write happen under the environment's lock.
func updateByocDesired(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, environmentId string, change func([]ampResource) ([]ampResource, error)) error {
	unlock := lockByocEnvironment(environmentId)
	defer unlock()
	desired, err := getByocDesired(ctx, apiClient, accountId, environmentId)
	if err != nil {
		return err
	}
	desired, err = change(desired)
	if err != nil {
		return err
	}
	return setByocDesired(ctx, apiClient, accountId, environmentId, desired)
}

func mustMarshal(v interface{}) json.RawMessage {
	if raw, ok := v.(json.RawMessage); ok {
		return raw
	}
	b, _ := json.Marshal(v)
	return b
}

// requireActiveByocEnvironment fails fast, rather than waiting out the timeout, for an environment
// whose agent will not act on desired state.
func requireActiveByocEnvironment(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, environmentId string) error {
	env, err := getByocEnvironment(ctx, apiClient, accountId, environmentId)
	if err != nil {
		return err
	}
	if reason, inactive := byocInactiveStates[env.State]; inactive {
		return fmt.Errorf("environment %s is %s: %s", environmentId, env.State, reason)
	}
	return nil
}

func byocClusterConfigFromPlan(plan ByocCluster) (ampClusterConfig, error) {
	return newAmpClusterConfig(plan.PgVersion, plan.Engine, plan.Extensions, plan.Password,
		plan.Cpu, plan.Memory, plan.ConnectivityMode, plan.AllowedCidrs, plan.PgSettings)
}

func (r resourceByocCluster) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan ByocCluster
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	accountId, getAccountOK, message := getAccountId(ctx, r.p.client)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}
	config, err := byocClusterConfigFromPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid cluster configuration", err.Error())
		return
	}
	environmentId := plan.EnvironmentID.Value
	clusterName := plan.ClusterName.Value
	if err := requireActiveByocEnvironment(ctx, r.p.client, accountId, environmentId); err != nil {
		resp.Diagnostics.AddError("Environment "+environmentId+" cannot run clusters", err.Error())
		return
	}

	scope := newAmpScope(r.p.client, accountId, environmentId)
	if err := scope.apply(ctx, "", ampIntentCreate, ampCreateClusterItems(scope.pool(), clusterName, config)); err != nil {
		resp.Diagnostics.AddError("Unable to create cluster "+clusterName, err.Error())
		return
	}
	if err := waitForAmp(ctx, scope, ampClusterTargets(scope.pool(), clusterName)); err != nil {
		resp.Diagnostics.AddError("Cluster "+clusterName+" did not become ready", err.Error())
		return
	}
	cluster, _, err := resourceByocClusterRead(ctx, scope, clusterName, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of cluster "+clusterName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cluster)...)
}

func resourceByocClusterRead(ctx context.Context, scope ampScope, clusterName string, prior ByocCluster) (cluster ByocCluster, found bool, err error) {
	snap, err := scope.snapshot(ctx)
	if err != nil {
		return cluster, false, err
	}
	status, ok, err := readAmpCluster(snap[0].Desired, snap[0].Observed, scope.pool(), clusterName)
	if err != nil || !ok {
		return cluster, ok, err
	}
	ep := status.Endpoint
	cluster = ByocCluster{
		AccountID:        types.String{Value: scope.accountId},
		EnvironmentID:    types.String{Value: scope.environmentId},
		ClusterName:      types.String{Value: clusterName},
		PgVersion:        types.Int64{Value: status.Config.PgVersion},
		Engine:           types.String{Value: status.Config.Engine},
		Extensions:       stringsToTf(status.Config.Extensions),
		Password:         tfString(ep.Config.Password),
		Cpu:              tfString(ep.Config.Cpu),
		Memory:           tfString(ep.Config.Memory),
		ConnectivityMode: tfString(ep.Config.ConnectivityMode),
		AllowedCidrs:     stringsToTf(ep.Config.AllowedCidrs),
		PgSettings:       mapToTf(ep.Config.PgSettings),
		ClusterID:        tfString(status.ClusterID),
		State:            tfString(ep.State),
		Host:             tfString(ep.Host),
		Port:             tfString(ep.Port),
		Username:         types.String{Value: ampUsername},
		Database:         types.String{Value: ampDatabase},
		TlsCaCertificate: tfString(ep.TlsCaCertificate),
	}
	if cluster.Password.Null && !prior.Password.Null {
		cluster.Password = prior.Password
	}
	return cluster, true, nil
}

func (r resourceByocCluster) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state ByocCluster
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

	scope := newAmpScope(r.p.client, accountId, state.EnvironmentID.Value)
	cluster, found, err := resourceByocClusterRead(ctx, scope, state.ClusterName.Value, state)
	if isYcpNotFound(err) || (err == nil && !found) {
		tflog.Warn(ctx, "Cluster "+state.ClusterName.Value+" no longer exists; removing it from state")
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of cluster "+state.ClusterName.Value, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cluster)...)
}

func (r resourceByocCluster) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan, state ByocCluster
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope := newAmpScope(r.p.client, state.AccountID.Value, state.EnvironmentID.Value)
	clusterName := state.ClusterName.Value

	config, err := byocClusterConfigFromPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid cluster configuration", err.Error())
		return
	}
	items := []ampPatchItem{{Kind: ampKindEndpoint, Name: ampEndpointName(scope.pool(), clusterName), Spec: config.Endpoint.spec(false)}}
	if err := scope.apply(ctx, "", ampIntentUpdate, items); err != nil {
		resp.Diagnostics.AddError("Unable to update cluster "+clusterName, err.Error())
		return
	}
	if err := waitForAmp(ctx, scope, ampClusterTargets(scope.pool(), clusterName)); err != nil {
		resp.Diagnostics.AddError("Cluster "+clusterName+" did not converge", err.Error())
		return
	}
	cluster, _, err := resourceByocClusterRead(ctx, scope, clusterName, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of cluster "+clusterName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cluster)...)
}

func (r resourceByocCluster) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state ByocCluster
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope := newAmpScope(r.p.client, state.AccountID.Value, state.EnvironmentID.Value)
	clusterName := state.ClusterName.Value
	clusterPath := ampClusterName(scope.pool(), clusterName)

	err := scope.apply(ctx, "", ampIntentDelete, []ampPatchItem{{Kind: ampKindCluster, Name: clusterPath}})
	if isYcpNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete cluster "+clusterName, err.Error())
		return
	}
	// The agent garbage-collects what desired state no longer declares.
	if err := waitForAmpGone(ctx, scope, ampKindCluster, clusterPath); err != nil {
		resp.Diagnostics.AddError("Unable to delete cluster "+clusterName, "The operation timed out waiting for the environment's agent to delete the cluster.")
		return
	}
	resp.State.RemoveResource(ctx)
}

// ImportState imports a cluster by "<environment_id>/<cluster_name>".
func (r resourceByocCluster) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <environment_id>/<cluster_name>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("environment_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("cluster_name"), parts[1])...)
}
