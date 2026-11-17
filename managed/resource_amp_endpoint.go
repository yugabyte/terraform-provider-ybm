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
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func (r *resourceAmpEndpoint) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := ampEndpointAttributes()
	for k, v := range ampParentAttributes() {
		attributes[k] = v
	}
	attributes["credentials"] = ampCredentialsAttribute(
		"The credentials of a read_write endpoint. Only the cluster's first read_write endpoint, normally the one on main, takes a password: " +
			"it sets the cluster's password, which branches and their endpoints inherit. A read_only endpoint takes none.")
	attributes["pg_settings"] = ampPgSettingsAttribute("PostgreSQL settings of the endpoint, such as work_mem. A key set here overrides the same key in its branch's settings.")
	attributes["branch_name"] = schema.StringAttribute{
		Description:   "The name of the branch the endpoint serves. Defaults to main. Changing it forces a new endpoint.",
		Optional:      true,
		Computed:      true,
		Validators:    []validator.String{stringvalidator.RegexMatches(ampNamePattern, ampNameRule)},
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseNonNullStateForUnknown(), stringplanmodifier.RequiresReplaceIfConfigured()},
	}
	attributes["endpoint_name"] = ampNameAttribute("The name of the endpoint, unique within its branch. Changing it forces a new endpoint.")
	attributes["role"] = schema.StringAttribute{
		Description:   "read_write or read_only. Defaults to read_only. A branch can have at most one read_write endpoint. Changing it forces a new endpoint.",
		Optional:      true,
		Computed:      true,
		Validators:    []validator.String{stringvalidator.OneOf(ampRoleReadWrite, ampRoleReadOnly)},
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseNonNullStateForUnknown(), stringplanmodifier.RequiresReplaceIfConfigured()},
	}
	attributes["endpoint_id"] = schema.StringAttribute{
		Description:   "The ID of the endpoint.",
		Computed:      true,
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseNonNullStateForUnknown()},
	}
	resp.Schema = schema.Schema{
		Description: `The resource to create an endpoint of a PostgreSQL cluster: compute that serves one branch, ` +
			`such as the cluster's read-write endpoint on main, a read-only replica, or the read-write endpoint of a ybm_amp_branch. ` +
			`Works for shared clusters (ybm_amp_cluster) and, with environment_id, BYOC clusters (ybm_amp_byoc_cluster).`,
		Attributes: attributes,
	}
}

func newResourceAmpEndpoint() resource.Resource {
	return &resourceAmpEndpoint{}
}

func (r *resourceAmpEndpoint) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_amp_endpoint"
}

func (r *resourceAmpEndpoint) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type resourceAmpEndpoint struct {
	p provider
}

func (r resourceAmpEndpoint) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(checkAmpParentPlan(ctx, req.Plan)...)
	// role defaults to read_only, so an unset one is not known in the plan.
	var role types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("role"), &role)...)
	roleName := ""
	if !role.IsUnknown() {
		roleName = stringOr(role, ampRoleReadOnly)
	}
	if c, ok := ampPlanCheckFrom(ctx, req, roleName); ok {
		resp.Diagnostics.Append(c.check()...)
	}
	resp.Diagnostics.Append(planAmpMemory(ctx, req, resp)...)
}

// planAmpMemory keeps an unset memory as it is unless cpu changes, when it follows the new cpu.
// Terraform would otherwise plan it as unknown on every update.
func planAmpMemory(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) diag.Diagnostics {
	var diags diag.Diagnostics
	if req.State.Raw.IsNull() {
		return diags
	}
	var configMemory, plannedCpu, priorCpu, priorMemory types.String
	diags.Append(req.Config.GetAttribute(ctx, path.Root("memory"), &configMemory)...)
	diags.Append(resp.Plan.GetAttribute(ctx, path.Root("cpu"), &plannedCpu)...)
	diags.Append(req.State.GetAttribute(ctx, path.Root("cpu"), &priorCpu)...)
	diags.Append(req.State.GetAttribute(ctx, path.Root("memory"), &priorMemory)...)
	if diags.HasError() || !configMemory.IsNull() || !plannedCpu.Equal(priorCpu) {
		return diags
	}
	diags.Append(resp.Plan.SetAttribute(ctx, path.Root("memory"), priorMemory)...)
	return diags
}

func stringOr(v types.String, fallback string) string {
	if known(v.IsNull(), v.IsUnknown()) {
		return v.ValueString()
	}
	return fallback
}

func (r resourceAmpEndpoint) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
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
	clusterName := plan.ClusterName.ValueString()
	branchName := stringOr(plan.BranchName, ampDefaultBranch)
	endpointName := plan.EndpointName.ValueString()
	role := stringOr(plan.Role, ampRoleReadOnly)
	config, err := newAmpEndpointConfig(role, plan, nil)
	if err != nil {
		resp.Diagnostics.AddError("Invalid endpoint configuration", err.Error())
		return
	}

	scope, region, err := ampChildScope(ctx, r.p, plan.EnvironmentID, plan.StoragePool, clusterName)
	if err == nil {
		err = checkAmpClusterID(ctx, scope, plan.ClusterID, clusterName)
	}
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
	plan.BranchName = types.StringValue(branchName)
	endpoint, found, err := resourceAmpEndpointRead(ctx, scope, plan)
	err = requireAmpFound(found, err, "endpoint "+endpointName)
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
	clusterName := prior.ClusterName.ValueString()
	branchName := stringOr(prior.BranchName, ampDefaultBranch)
	endpointPath := ampEndpointPath(scope.pool(), clusterName, branchName, prior.EndpointName.ValueString())
	env, ok := ampEnvDeclaring(snap, ampKindEndpoint, endpointPath)
	if !ok {
		return endpoint, false, nil
	}
	status, _, err := readAmpEndpoint(env.Desired, env.Observed, endpointPath)
	if err != nil {
		return endpoint, true, err
	}
	endpoint = AmpEndpoint{
		AccountID:             types.StringValue(scope.accountId),
		EnvironmentID:         optionalString(prior.EnvironmentID),
		StoragePool:           optionalString(prior.StoragePool),
		ClusterName:           types.StringValue(clusterName),
		ClusterID:             ampClusterIDToTf(snap, scope.pool(), clusterName, prior.ClusterID),
		BranchName:            types.StringValue(branchName),
		EndpointName:          prior.EndpointName,
		Role:                  types.StringValue(status.Config.Role),
		Credentials:           ampCredentialsToTf(prior.Credentials),
		Cpu:                   tfString(status.Config.Cpu),
		Memory:                tfString(status.Config.Memory),
		SuspendTimeoutSeconds: types.Int64Null(),
		ConnectivityMode:      tfString(status.Config.ConnectivityMode),
		AllowedCidrs:          stringsToTf(derefStrings(status.Config.AllowedCidrs), prior.AllowedCidrs),
		PgSettings:            mapToTf(derefMap(status.Config.PgSettings), prior.PgSettings),
		EndpointID:            tfString(status.EndpointID),
		State:                 tfString(status.State),
		Host:                  tfString(status.Host),
		Port:                  tfString(status.Port),
		Username:              types.StringValue(ampUsername),
		Database:              types.StringValue(ampDatabase),
		TlsCaCertificate:      tfString(status.TlsCaCertificate),
	}
	return endpoint, true, nil
}

func (r resourceAmpEndpoint) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AmpEndpoint
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	accountId := state.AccountID.ValueString()
	if accountId == "" {
		var ok bool
		var message string
		accountId, ok, message = getAccountId(ctx, r.p.client)
		if !ok {
			resp.Diagnostics.AddError("Unable to get account ID", message)
			return
		}
	}
	scope := newAmpScope(r.p.client, accountId, state.EnvironmentID.ValueString(), state.StoragePool.ValueString())
	endpoint, found, err := resourceAmpEndpointRead(ctx, scope, state)
	if isYcpNotFound(err) || (err == nil && !found) {
		tflog.Warn(ctx, "Endpoint "+state.EndpointName.ValueString()+" no longer exists; removing it from state")
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of endpoint "+state.EndpointName.ValueString(), err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &endpoint)...)
}

func (r resourceAmpEndpoint) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state AmpEndpoint
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpointName := state.EndpointName.ValueString()
	config, err := newAmpEndpointConfig(state.Role.ValueString(), plan, &state)
	if err != nil {
		resp.Diagnostics.AddError("Invalid endpoint configuration", err.Error())
		return
	}
	scope, region, err := ampChildScope(ctx, r.p, state.EnvironmentID, state.StoragePool, state.ClusterName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update endpoint "+endpointName, err.Error())
		return
	}
	endpointPath := ampEndpointPath(scope.pool(), state.ClusterName.ValueString(), state.BranchName.ValueString(), endpointName)
	items := []ampPatchItem{{Kind: ampKindEndpoint, Name: endpointPath, Spec: config.spec(false)}}
	if err := scope.apply(ctx, region, ampIntentUpdate, items); err != nil {
		resp.Diagnostics.AddError("Unable to update endpoint "+endpointName, err.Error())
		return
	}
	if err := waitForAmp(ctx, scope, []ampTarget{{Kind: ampKindEndpoint, Name: endpointPath}}); err != nil {
		resp.Diagnostics.AddError("Endpoint "+endpointName+" did not converge", err.Error())
		return
	}
	endpoint, found, err := resourceAmpEndpointRead(ctx, scope, plan)
	err = requireAmpFound(found, err, "endpoint "+endpointName)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of endpoint "+endpointName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &endpoint)...)
}

func (r resourceAmpEndpoint) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AmpEndpoint
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpointName := state.EndpointName.ValueString()
	scope := newAmpScope(r.p.client, state.AccountID.ValueString(), state.EnvironmentID.ValueString(), state.StoragePool.ValueString())
	region, err := scope.clusterRegion(ctx, state.ClusterName.ValueString())
	if isYcpNotFound(err) {
		// Deleting the cluster already took the endpoint with it.
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete endpoint "+endpointName, err.Error())
		return
	}
	endpointPath := ampEndpointPath(scope.pool(), state.ClusterName.ValueString(), state.BranchName.ValueString(), endpointName)
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
func (r resourceAmpEndpoint) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
