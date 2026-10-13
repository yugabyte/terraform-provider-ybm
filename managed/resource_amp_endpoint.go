/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type resourceAmpEndpointType struct{}

func (r resourceAmpEndpointType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	attributes := ampEndpointAttributes()
	for k, v := range ampParentAttributes() {
		attributes[k] = v
	}
	attributes["credentials"] = ampCredentialsAttribute(
		"The credentials of a read_write endpoint, whose password is required. A read_only endpoint takes none; it uses the read_write endpoint's.", false)
	attributes["pg_settings"] = ampPgSettingsAttribute("PostgreSQL settings of the endpoint, such as work_mem. A key set here overrides the same key in its branch's settings.")
	attributes["branch_name"] = tfsdk.Attribute{
		Description:   "The name of the branch the endpoint serves. Defaults to main. Changing it forces a new endpoint.",
		Type:          types.StringType,
		Optional:      true,
		Computed:      true,
		Validators:    []tfsdk.AttributeValidator{stringvalidator.RegexMatches(ampNamePattern, ampNameRule)},
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown(), tfsdk.RequiresReplace()},
	}
	attributes["endpoint_name"] = ampNameAttribute("The name of the endpoint, unique within its branch. Changing it forces a new endpoint.")
	attributes["role"] = tfsdk.Attribute{
		Description:   "read_write or read_only. Defaults to read_only. A branch can have at most one read_write endpoint, and the cluster resources already create one on main. Changing it forces a new endpoint.",
		Type:          types.StringType,
		Optional:      true,
		Computed:      true,
		Validators:    []tfsdk.AttributeValidator{stringvalidator.OneOf(ampRoleReadWrite, ampRoleReadOnly)},
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown(), tfsdk.RequiresReplace()},
	}
	attributes["endpoint_id"] = tfsdk.Attribute{
		Description:   "The ID of the endpoint.",
		Type:          types.StringType,
		Computed:      true,
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
	}
	return tfsdk.Schema{
		Description: `The resource to create an endpoint of an AMP PostgreSQL cluster: compute that serves one branch, ` +
			`such as a read-only replica of main or the read-write endpoint of a ybm_amp_branch. ` +
			`Works for shared clusters (ybm_amp_cluster) and, with environment_id, BYOC clusters (ybm_byoc_cluster).`,
		Attributes: attributes,
	}, nil
}

func (r resourceAmpEndpointType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceAmpEndpoint{
		p: *(p.(*provider)),
	}, nil
}

type resourceAmpEndpoint struct {
	p provider
}

func ampEndpointConfigFromPlan(plan AmpEndpoint, role string) (ampEndpointConfig, error) {
	return newAmpEndpointConfig(role, plan.Credentials, plan.Cpu, plan.Memory, plan.ConnectivityMode, plan.AllowedCidrs, plan.PgSettings)
}

func (r resourceAmpEndpoint) ModifyPlan(ctx context.Context, req tfsdk.ModifyResourcePlanRequest, resp *tfsdk.ModifyResourcePlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(checkAmpParentPlan(ctx, req.Plan)...)
	// role defaults to read_only, so an unset one is not known in the plan.
	var role types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("role"), &role)...)
	roleName := ""
	if !role.Unknown {
		roleName = stringOr(role, ampRoleReadOnly)
	}
	if c, ok := ampPlanCheckFrom(ctx, req, roleName); ok {
		resp.Diagnostics.Append(c.check()...)
	}
}

func stringOr(v types.String, fallback string) string {
	if known(v.Null, v.Unknown) {
		return v.Value
	}
	return fallback
}

func (r resourceAmpEndpoint) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}
	var plan AmpEndpoint
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	clusterName := plan.ClusterName.Value
	branchName := stringOr(plan.BranchName, ampDefaultBranch)
	endpointName := plan.EndpointName.Value
	role := stringOr(plan.Role, ampRoleReadOnly)
	config, err := ampEndpointConfigFromPlan(plan, role)
	if err != nil {
		resp.Diagnostics.AddError("Invalid endpoint configuration", err.Error())
		return
	}

	scope, region, err := ampChildScope(ctx, r.p, plan.EnvironmentID, plan.StoragePool, clusterName)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create endpoint "+endpointName, err.Error())
		return
	}
	endpointPath := ampEndpointPath(scope.pool(), clusterName, branchName, endpointName)
	items := []ampPatchItem{{Kind: ampKindEndpoint, Name: endpointPath, Spec: config.spec(true)}}
	if err := scope.apply(ctx, region, ampIntentCreate, items); err != nil {
		resp.Diagnostics.AddError("Unable to create endpoint "+endpointName, err.Error())
		return
	}
	waitErr := waitForAmp(ctx, scope, []ampTarget{{Kind: ampKindEndpoint, Name: endpointPath}})
	// The endpoint exists from here on, so its state is saved even when it did not become ready:
	// Terraform then marks it tainted rather than losing track of it.
	plan.BranchName = types.String{Value: branchName}
	endpoint, _, err := resourceAmpEndpointRead(ctx, scope, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of endpoint "+endpointName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &endpoint)...)
	if waitErr != nil {
		resp.Diagnostics.AddError("Endpoint "+endpointName+" did not become ready", waitErr.Error())
	}
}

func resourceAmpEndpointRead(ctx context.Context, scope ampScope, prior AmpEndpoint) (endpoint AmpEndpoint, found bool, err error) {
	snap, err := scope.snapshot(ctx)
	if err != nil {
		return endpoint, false, err
	}
	clusterName := prior.ClusterName.Value
	branchName := stringOr(prior.BranchName, ampDefaultBranch)
	endpointPath := ampEndpointPath(scope.pool(), clusterName, branchName, prior.EndpointName.Value)
	env, ok := ampEnvDeclaring(snap, ampKindEndpoint, endpointPath)
	if !ok {
		return endpoint, false, nil
	}
	status, _, err := readAmpEndpoint(env.Desired, env.Observed, endpointPath)
	if err != nil {
		return endpoint, true, err
	}
	endpoint = AmpEndpoint{
		AccountID:             types.String{Value: scope.accountId},
		EnvironmentID:         optionalString(prior.EnvironmentID),
		StoragePool:           optionalString(prior.StoragePool),
		ClusterName:           types.String{Value: clusterName},
		BranchName:            types.String{Value: branchName},
		EndpointName:          prior.EndpointName,
		Role:                  types.String{Value: status.Config.Role},
		Credentials:           ampCredentialsToTf(status.Config.Password, prior.Credentials),
		Cpu:                   tfString(status.Config.Cpu),
		Memory:                tfString(status.Config.Memory),
		SuspendTimeoutSeconds: types.Int64{Null: true},
		ConnectivityMode:      tfString(status.Config.ConnectivityMode),
		AllowedCidrs:          stringsToTf(status.Config.AllowedCidrs, prior.AllowedCidrs),
		PgSettings:            mapToTf(status.Config.PgSettings, prior.PgSettings),
		EndpointID:            tfString(status.EndpointID),
		State:                 tfString(status.State),
		Host:                  tfString(status.Host),
		Port:                  tfString(status.Port),
		Username:              types.String{Value: ampUsername},
		Database:              types.String{Value: ampDatabase},
		TlsCaCertificate:      tfString(status.TlsCaCertificate),
	}
	return endpoint, true, nil
}

func (r resourceAmpEndpoint) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state AmpEndpoint
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
	endpoint, found, err := resourceAmpEndpointRead(ctx, scope, state)
	if isYcpNotFound(err) || (err == nil && !found) {
		tflog.Warn(ctx, "Endpoint "+state.EndpointName.Value+" no longer exists; removing it from state")
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of endpoint "+state.EndpointName.Value, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &endpoint)...)
}

func (r resourceAmpEndpoint) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan, state AmpEndpoint
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpointName := state.EndpointName.Value
	config, err := ampEndpointConfigFromPlan(plan, state.Role.Value)
	if err != nil {
		resp.Diagnostics.AddError("Invalid endpoint configuration", err.Error())
		return
	}
	scope, region, err := ampChildScope(ctx, r.p, state.EnvironmentID, state.StoragePool, state.ClusterName.Value)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update endpoint "+endpointName, err.Error())
		return
	}
	endpointPath := ampEndpointPath(scope.pool(), state.ClusterName.Value, state.BranchName.Value, endpointName)
	items := []ampPatchItem{{Kind: ampKindEndpoint, Name: endpointPath, Spec: config.spec(false)}}
	if err := scope.apply(ctx, region, ampIntentUpdate, items); err != nil {
		resp.Diagnostics.AddError("Unable to update endpoint "+endpointName, err.Error())
		return
	}
	if err := waitForAmp(ctx, scope, []ampTarget{{Kind: ampKindEndpoint, Name: endpointPath}}); err != nil {
		resp.Diagnostics.AddError("Endpoint "+endpointName+" did not converge", err.Error())
		return
	}
	endpoint, _, err := resourceAmpEndpointRead(ctx, scope, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of endpoint "+endpointName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &endpoint)...)
}

func (r resourceAmpEndpoint) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state AmpEndpoint
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpointName := state.EndpointName.Value
	scope := newAmpScope(r.p.client, state.AccountID.Value, state.EnvironmentID.Value, state.StoragePool.Value)
	region, err := scope.clusterRegion(ctx, state.ClusterName.Value)
	if isYcpNotFound(err) {
		// Deleting the cluster already took the endpoint with it.
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete endpoint "+endpointName, err.Error())
		return
	}
	endpointPath := ampEndpointPath(scope.pool(), state.ClusterName.Value, state.BranchName.Value, endpointName)
	if err := scope.apply(ctx, region, ampIntentDelete, []ampPatchItem{{Kind: ampKindEndpoint, Name: endpointPath}}); err != nil && !isYcpNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete endpoint "+endpointName, err.Error())
		return
	}
	if err := waitForAmpGone(ctx, scope, []ampTarget{{Kind: ampKindEndpoint, Name: endpointPath}}); err != nil {
		resp.Diagnostics.AddError("Unable to delete endpoint "+endpointName, err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

// ImportState imports an endpoint by "<cluster_name>/<branch_name>/<endpoint_name>", or
// "<environment_id>/<storage_pool>/<cluster_name>/<branch_name>/<endpoint_name>" for a BYOC cluster.
func (r resourceAmpEndpoint) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) == 5 && parts[0] != "" && parts[1] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("environment_id"), parts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("storage_pool"), parts[1])...)
		parts = parts[2:]
	}
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected <cluster_name>/<branch_name>/<endpoint_name> or <environment_id>/<storage_pool>/<cluster_name>/<branch_name>/<endpoint_name>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("cluster_name"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("branch_name"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("endpoint_name"), parts[2])...)
}
