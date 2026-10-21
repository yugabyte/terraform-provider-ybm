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

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

const byocEnvironmentReady = "READY"

// Environment states in which the agent will never act on desired state.
var byocInactiveStates = map[string]string{
	"SUSPENDED":    "it is suspended",
	"DRAINING":     "it is draining",
	"DEREGISTERED": "it is deregistered",
}

func ampStoragePoolAttribute(description string, required bool) tfsdk.Attribute {
	return tfsdk.Attribute{
		Description:   description,
		Type:          types.StringType,
		Required:      required,
		Optional:      !required,
		Validators:    []tfsdk.AttributeValidator{stringvalidator.RegexMatches(ampNamePattern, ampNameRule)},
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.RequiresReplace()},
	}
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
	attributes["storage_pool"] = ampStoragePoolAttribute(
		"The name of the storage pool to create the cluster in, such as a ybm_byoc_storage_pool's name. Changing it forces a new cluster.", true)
	return tfsdk.Schema{
		Description: `The resource to create a PostgreSQL cluster in a storage pool of a BYOC environment registered with ybm_byoc_environment. ` +
			`The cluster is created with one read-write endpoint on its main branch once the environment's agent picks it up. ` +
			`Add branches and endpoints with ybm_aeon_pg_branch and ybm_aeon_pg_endpoint.`,
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

// waitForActiveByocEnvironment waits for the environment's agent to enroll, so the environment,
// the agent's install and the cluster can all come from one apply. It fails at once for an
// environment whose agent will never act on desired state.
func waitForActiveByocEnvironment(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, environmentId string) error {
	return ampPoll(ctx, "environment "+environmentId+" to become READY", func(ctx context.Context) (string, error) {
		env, err := getByocEnvironment(ctx, apiClient, accountId, environmentId)
		if err != nil {
			return "", err
		}
		if reason, inactive := byocInactiveStates[env.State]; inactive {
			return "", fmt.Errorf("environment %s is %s: %s", environmentId, env.State, reason)
		}
		if env.State != byocEnvironmentReady {
			return fmt.Sprintf("environment %s is %s; install its agent with the environment's install_hint", environmentId, env.State), nil
		}
		return "", nil
	})
}

func byocClusterConfigFromPlan(plan ByocCluster, prior *ByocCluster) ampClusterConfig {
	priorPg := types.Map{Null: true}
	if prior != nil {
		priorPg = prior.BranchPgSettings
	}
	return newAmpClusterConfig(plan.PgVersion, plan.Engine, plan.Extensions, plan.BranchPgSettings, plan.BranchExtensions, priorPg)
}

func (r resourceByocCluster) ModifyPlan(ctx context.Context, req tfsdk.ModifyResourcePlanRequest, resp *tfsdk.ModifyResourcePlanResponse) {
	modifyAmpClusterPlan(ctx, req, resp)
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
	environmentId := plan.EnvironmentID.Value
	clusterName := plan.ClusterName.Value
	if err := waitForActiveByocEnvironment(ctx, r.p.client, accountId, environmentId); err != nil {
		resp.Diagnostics.AddError("Environment "+environmentId+" cannot run clusters", err.Error())
		return
	}

	scope := newAmpScope(r.p.client, accountId, environmentId, plan.StoragePool.Value)
	created, createErr := createAmpCluster(ctx, scope, "", clusterName, byocClusterConfigFromPlan(plan, nil))
	if !created {
		resp.Diagnostics.AddError("Unable to create cluster "+clusterName, createErr.Error())
		return
	}
	// The cluster exists from here on, so its state is saved even when the create did not finish:
	// Terraform then marks it tainted rather than losing track of it.
	cluster, _, err := resourceByocClusterRead(ctx, scope, clusterName, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of cluster "+clusterName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cluster)...)
	if createErr != nil {
		resp.Diagnostics.AddError("Unable to create cluster "+clusterName, createErr.Error())
	}
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
	cluster = ByocCluster{
		AccountID:        types.String{Value: scope.accountId},
		EnvironmentID:    types.String{Value: scope.environmentId},
		StoragePool:      types.String{Value: scope.pool()},
		ClusterName:      types.String{Value: clusterName},
		PgVersion:        tfInt64(status.Config.PgVersion),
		Engine:           tfString(status.Config.Engine),
		Extensions:       stringsToTf(status.Config.Extensions, prior.Extensions),
		BranchPgSettings: mapToTf(derefMap(status.Config.BranchPgSettings), prior.BranchPgSettings),
		BranchExtensions: stringsToTf(status.Config.BranchExtensions, prior.BranchExtensions),
		ForceDestroy:     prior.ForceDestroy,
		ClusterID:        tfString(status.ClusterID),
		State:            tfString(status.State),
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

	scope := newAmpScope(r.p.client, accountId, state.EnvironmentID.Value, state.StoragePool.Value)
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
	scope := newAmpScope(r.p.client, state.AccountID.Value, state.EnvironmentID.Value, state.StoragePool.Value)
	clusterName := state.ClusterName.Value
	// The agent will never act on a change to an environment that is not READY, so this fails at
	// once rather than after the whole wait.
	if err := waitForActiveByocEnvironment(ctx, r.p.client, scope.accountId, scope.environmentId); err != nil {
		resp.Diagnostics.AddError("Environment "+scope.environmentId+" cannot run clusters", err.Error())
		return
	}
	if err := updateAmpCluster(ctx, scope, "", clusterName, byocClusterConfigFromPlan(plan, &state)); err != nil {
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
	scope := newAmpScope(r.p.client, state.AccountID.Value, state.EnvironmentID.Value, state.StoragePool.Value)
	clusterName := state.ClusterName.Value
	// The agent garbage-collects what desired state no longer declares.
	err := deleteAmpCluster(ctx, scope, "", clusterName, boolValue(state.ForceDestroy))
	if err != nil && !isYcpNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete cluster "+clusterName, err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

// ImportState imports a cluster by "<environment_id>/<storage_pool>/<cluster_name>".
func (r resourceByocCluster) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <environment_id>/<storage_pool>/<cluster_name>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("environment_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("storage_pool"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("cluster_name"), parts[2])...)
}
