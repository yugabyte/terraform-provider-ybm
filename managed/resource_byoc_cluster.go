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
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	retry "github.com/sethvargo/go-retry"
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
			`The cluster is created with one read-write endpoint once the environment's agent picks it up.`,
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
	b, _ := json.Marshal(v)
	return b
}

// withByocCluster returns desired with the cluster and its endpoint set to config. The pool is the
// account ID, as for AMP Managed clusters, and is declared once for every cluster in it.
func withByocCluster(desired []ampResource, pool string, clusterName string, config ampClusterConfig, create bool) ([]ampResource, error) {
	clusterPath := ampClusterName(pool, clusterName)
	endpointPath := ampEndpointName(pool, clusterName)
	_, exists := findAmpDesired(desired, ampKindCluster, clusterPath)
	if create && exists {
		return nil, fmt.Errorf("cluster %s already exists in the environment", clusterName)
	}
	if !create && !exists {
		return nil, fmt.Errorf("cluster %s no longer exists in the environment", clusterName)
	}

	if _, ok := findAmpDesired(desired, ampKindStoragePool, pool); !ok {
		desired = append(desired, ampResource{Kind: ampKindStoragePool, Name: pool, Spec: mustMarshal(map[string]string{"owner_user_id": pool})})
	}
	if create {
		desired = append(desired, ampResource{Kind: ampKindCluster, Name: clusterPath, Spec: mustMarshal(config.clusterSpec())})
	}
	endpointSpec := mustMarshal(config.endpointSpec(true))
	for i := range desired {
		if desired[i].Kind == ampKindEndpoint && desired[i].Name == endpointPath {
			desired[i].Spec = endpointSpec
			return desired, nil
		}
	}
	return append(desired, ampResource{Kind: ampKindEndpoint, Name: endpointPath, Spec: endpointSpec}), nil
}

// withoutByocCluster returns desired without the cluster and everything under it, and without the
// pool once no cluster is left in it.
func withoutByocCluster(desired []ampResource, pool string, clusterName string) []ampResource {
	clusterPath := ampClusterName(pool, clusterName)
	kept := make([]ampResource, 0, len(desired))
	poolUsed := false
	for _, r := range desired {
		if r.Name == clusterPath || strings.HasPrefix(r.Name, clusterPath+"/") {
			continue
		}
		if strings.HasPrefix(r.Name, pool+"/") {
			poolUsed = true
		}
		kept = append(kept, r)
	}
	if poolUsed {
		return kept
	}
	out := kept[:0]
	for _, r := range kept {
		if !(r.Kind == ampKindStoragePool && r.Name == pool) {
			out = append(out, r)
		}
	}
	return out
}

func byocClusterConfigFromPlan(plan ByocCluster) (ampClusterConfig, error) {
	return newAmpClusterConfig(plan.PgVersion, plan.Engine, plan.Extensions, plan.Password,
		plan.Cpu, plan.Memory, plan.ConnectivityMode, plan.AllowedCidrs)
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
	apiClient := r.p.client

	accountId, getAccountOK, message := getAccountId(ctx, apiClient)
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

	// Fail now rather than wait out the timeout for an agent that will not act.
	env, err := getByocEnvironment(ctx, apiClient, accountId, environmentId)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read environment "+environmentId, err.Error())
		return
	}
	if reason, inactive := byocInactiveStates[env.State]; inactive {
		resp.Diagnostics.AddError("Environment "+environmentId+" cannot run clusters",
			fmt.Sprintf("The environment is %s: %s.", env.State, reason))
		return
	}

	err = updateByocDesired(ctx, apiClient, accountId, environmentId, func(desired []ampResource) ([]ampResource, error) {
		return withByocCluster(desired, accountId, clusterName, config, true)
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create cluster "+clusterName, err.Error())
		return
	}

	cluster, err := waitForByocCluster(ctx, apiClient, accountId, environmentId, clusterName, plan)
	if err != nil {
		resp.Diagnostics.AddError("Cluster "+clusterName+" did not become ready", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cluster)...)
}

func resourceByocClusterRead(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, environmentId string, clusterName string, prior ByocCluster) (cluster ByocCluster, found bool, err error) {
	desired, err := getByocDesired(ctx, apiClient, accountId, environmentId)
	if err != nil {
		return cluster, false, err
	}
	env, err := getByocEnvironment(ctx, apiClient, accountId, environmentId)
	if err != nil {
		return cluster, false, err
	}
	status, ok, err := readAmpCluster(desired, env.Resources, accountId, clusterName)
	if err != nil || !ok {
		return cluster, ok, err
	}
	cluster = ByocCluster{
		AccountID:        types.String{Value: accountId},
		EnvironmentID:    types.String{Value: environmentId},
		ClusterName:      types.String{Value: clusterName},
		PgVersion:        types.Int64{Value: status.Config.PgVersion},
		Engine:           types.String{Value: status.Config.Engine},
		Extensions:       stringsToTf(status.Config.Extensions),
		Password:         tfString(status.Config.Password),
		Cpu:              tfString(status.Config.Cpu),
		Memory:           tfString(status.Config.Memory),
		ConnectivityMode: tfString(status.Config.ConnectivityMode),
		AllowedCidrs:     stringsToTf(status.Config.AllowedCidrs),
		ClusterID:        tfString(status.ClusterID),
		State:            tfString(status.State),
		Host:             tfString(status.Host),
		Port:             tfString(status.Port),
		Username:         types.String{Value: ampUsername},
		Database:         types.String{Value: ampDatabase},
		TlsCaCertificate: tfString(status.TlsCaCertificate),
	}
	if cluster.Password.Null && !prior.Password.Null {
		cluster.Password = prior.Password
	}
	return cluster, true, nil
}

// waitForByocCluster polls until the environment's agent reports the cluster's read-write
// endpoint as converged, then returns the cluster as read.
func waitForByocCluster(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, environmentId string, clusterName string, prior ByocCluster) (ByocCluster, error) {
	var terminal error
	retryPolicy := retry.WithMaxDuration(ampOperationTimeout, retry.NewConstant(ampPollInterval))
	err := retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		desired, err := getByocDesired(ctx, apiClient, accountId, environmentId)
		if err != nil {
			return retry.RetryableError(err)
		}
		env, err := getByocEnvironment(ctx, apiClient, accountId, environmentId)
		if err != nil {
			return retry.RetryableError(err)
		}
		done, err := ampEndpointProgress(desired, env.Resources, accountId, clusterName)
		if err != nil {
			terminal = err
			return err
		}
		if done {
			return nil
		}
		tflog.Debug(ctx, fmt.Sprintf("Cluster %s has not converged yet; environment %s is %s", clusterName, environmentId, env.State))
		return retry.RetryableError(errors.New("the cluster is not ready yet"))
	})
	if terminal != nil {
		return ByocCluster{}, terminal
	}
	if err != nil {
		return ByocCluster{}, fmt.Errorf("timed out after %v waiting for the environment's agent to bring the cluster up; check that the agent is running", ampOperationTimeout)
	}
	cluster, _, err := resourceByocClusterRead(ctx, apiClient, accountId, environmentId, clusterName, prior)
	return cluster, err
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

	cluster, found, err := resourceByocClusterRead(ctx, r.p.client, accountId, state.EnvironmentID.Value, state.ClusterName.Value, state)
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
	apiClient := r.p.client
	accountId := state.AccountID.Value
	environmentId := state.EnvironmentID.Value
	clusterName := state.ClusterName.Value

	config, err := byocClusterConfigFromPlan(plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid cluster configuration", err.Error())
		return
	}
	err = updateByocDesired(ctx, apiClient, accountId, environmentId, func(desired []ampResource) ([]ampResource, error) {
		return withByocCluster(desired, accountId, clusterName, config, false)
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to update cluster "+clusterName, err.Error())
		return
	}

	cluster, err := waitForByocCluster(ctx, apiClient, accountId, environmentId, clusterName, plan)
	if err != nil {
		resp.Diagnostics.AddError("Cluster "+clusterName+" did not converge", err.Error())
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
	apiClient := r.p.client
	accountId := state.AccountID.Value
	environmentId := state.EnvironmentID.Value
	clusterName := state.ClusterName.Value

	err := updateByocDesired(ctx, apiClient, accountId, environmentId, func(desired []ampResource) ([]ampResource, error) {
		return withoutByocCluster(desired, accountId, clusterName), nil
	})
	if isYcpNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete cluster "+clusterName, err.Error())
		return
	}

	// The agent garbage-collects what desired state no longer declares.
	retryPolicy := retry.WithMaxDuration(ampOperationTimeout, retry.NewConstant(ampPollInterval))
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		env, err := getByocEnvironment(ctx, apiClient, accountId, environmentId)
		if isYcpNotFound(err) {
			return nil
		}
		if err != nil {
			return retry.RetryableError(err)
		}
		if _, ok := findAmpObserved(env.Resources, ampKindCluster, ampClusterName(accountId, clusterName)); ok {
			return retry.RetryableError(errors.New("the cluster is still being deleted"))
		}
		return nil
	})
	if err != nil {
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
