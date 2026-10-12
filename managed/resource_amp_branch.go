/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var ampLsnPattern = regexp.MustCompile(`^[0-9A-Fa-f]+/[0-9A-Fa-f]+$`)

// ampParentAttributes are the attributes that place a branch or endpoint in its cluster.
func ampParentAttributes() map[string]tfsdk.Attribute {
	return map[string]tfsdk.Attribute{
		"account_id": {
			Description:   "The ID of the account the cluster belongs to.",
			Type:          types.StringType,
			Computed:      true,
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
		},
		"environment_id": {
			Description:   "The ID of the BYOC environment the cluster is in. Leave it unset for a shared (AMP Managed) cluster. Changing it forces a replacement.",
			Type:          types.StringType,
			Optional:      true,
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.RequiresReplace()},
		},
		"cluster_name": ampNameAttribute("The name of the cluster. Changing it forces a replacement."),
	}
}

// ampChildScope returns the scope of the cluster a branch or endpoint belongs to, and the region
// to write it in. A BYOC environment must be able to act on desired state.
func ampChildScope(ctx context.Context, p provider, environmentId types.String, clusterName string) (ampScope, string, error) {
	accountId, ok, message := getAccountId(ctx, p.client)
	if !ok {
		return ampScope{}, "", fmt.Errorf("unable to get account ID: %s", message)
	}
	envId := ""
	if known(environmentId.Null, environmentId.Unknown) {
		envId = environmentId.Value
	}
	scope := newAmpScope(p.client, accountId, envId)
	if scope.isByoc() {
		if err := requireActiveByocEnvironment(ctx, p.client, accountId, envId); err != nil {
			return scope, "", err
		}
	}
	region, err := scope.clusterRegion(ctx, clusterName)
	return scope, region, err
}

func optionalString(v types.String) types.String {
	if v.Unknown {
		return types.String{Null: true}
	}
	return v
}

type resourceAmpBranchType struct{}

func (r resourceAmpBranchType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	attributes := ampParentAttributes()
	attributes["branch_name"] = ampNameAttribute("The name of the branch. The default branch, main, is created with the cluster and cannot be managed here. Changing it forces a new branch.")
	attributes["parent_branch"] = tfsdk.Attribute{
		Description:   "The name of the branch to fork from. Defaults to main. Changing it forces a new branch.",
		Type:          types.StringType,
		Optional:      true,
		Computed:      true,
		Validators:    []tfsdk.AttributeValidator{stringvalidator.RegexMatches(ampNamePattern, ampNameRule)},
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown(), tfsdk.RequiresReplace()},
	}
	attributes["fork_point_lsn"] = tfsdk.Attribute{
		Description:   "The LSN of the parent branch to fork at, such as 0/16B3748. Defaults to the parent's latest. Changing it forces a new branch.",
		Type:          types.StringType,
		Optional:      true,
		Validators:    []tfsdk.AttributeValidator{stringvalidator.RegexMatches(ampLsnPattern, "must be an LSN of the form X/X")},
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.RequiresReplace()},
	}
	attributes["pg_settings"] = ampPgSettingsAttribute("PostgreSQL settings of the branch, such as work_mem. Can be changed in place.")
	attributes["extensions"] = tfsdk.Attribute{
		Description: "Extensions to install in the branch. Extensions can be added in place but not removed.",
		Type:        types.ListType{ElemType: types.StringType},
		Optional:    true,
	}
	attributes["branch_id"] = tfsdk.Attribute{
		Description:   "The ID of the branch.",
		Type:          types.StringType,
		Computed:      true,
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
	}
	attributes["state"] = tfsdk.Attribute{
		Description: "The state the branch last reported.",
		Type:        types.StringType,
		Computed:    true,
	}
	return tfsdk.Schema{
		Description: `The resource to create a branch of an AMP PostgreSQL cluster: a copy-on-write fork of another branch ` +
			`with its own settings. Works for shared clusters (ybm_amp_cluster) and, with environment_id, BYOC clusters (ybm_byoc_cluster). ` +
			`Connect to a branch through a ybm_amp_endpoint on it.`,
		Attributes: attributes,
	}, nil
}

func (r resourceAmpBranchType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceAmpBranch{
		p: *(p.(*provider)),
	}, nil
}

type resourceAmpBranch struct {
	p provider
}

// ampObservedID returns the AMP ID of a resource the agent has reported.
func ampObservedID(snap []ampEnvironmentResources, kind, name string) string {
	for _, env := range snap {
		if o, ok := findAmpObserved(env.Observed, kind, name); ok {
			return o.Attrs["id"]
		}
	}
	return ""
}

// ampBranchNameByID returns the name of the cluster's branch with the given AMP ID.
func ampBranchNameByID(observed []ampObserved, clusterPath, id string) string {
	for _, o := range observed {
		if o.Kind == ampKindBranch && o.Attrs["id"] == id && strings.HasPrefix(o.ID, ampKindBranch+"/"+clusterPath+"/") {
			return strings.TrimPrefix(o.ID, ampKindBranch+"/"+clusterPath+"/")
		}
	}
	return ""
}

func (r resourceAmpBranch) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}
	var plan AmpBranch
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	clusterName, branchName := plan.ClusterName.Value, plan.BranchName.Value
	if branchName == ampDefaultBranch {
		resp.Diagnostics.AddError("Invalid branch name", "The main branch is created with the cluster and cannot be managed as a ybm_amp_branch.")
		return
	}
	scope, region, err := ampChildScope(ctx, r.p, plan.EnvironmentID, clusterName)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create branch "+branchName, err.Error())
		return
	}
	pool := scope.pool()
	branchPath := ampBranchName(pool, clusterName, branchName)

	parent := ampDefaultBranch
	if known(plan.ParentBranch.Null, plan.ParentBranch.Unknown) {
		parent = plan.ParentBranch.Value
	}
	spec := ampBranchSpec{ForkPointLsn: plan.ForkPointLsn.Value}
	if parent != ampDefaultBranch {
		snap, err := scope.snapshot(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Unable to create branch "+branchName, err.Error())
			return
		}
		spec.ParentBranchID = ampObservedID(snap, ampKindBranch, ampBranchName(pool, clusterName, parent))
		if spec.ParentBranchID == "" {
			resp.Diagnostics.AddError("Unable to create branch "+branchName, fmt.Sprintf("parent branch %s does not exist yet", parent))
			return
		}
	}
	if pg := mapFromTf(plan.PgSettings); len(pg) > 0 {
		spec.PgSettings = &pg
	}

	items := []ampPatchItem{{Kind: ampKindBranch, Name: branchPath, Spec: spec}}
	if err := scope.apply(ctx, region, ampIntentCreate, items); err != nil {
		resp.Diagnostics.AddError("Unable to create branch "+branchName, err.Error())
		return
	}
	targets := []ampTarget{{Kind: ampKindBranch, Name: branchPath}}
	if err := waitForAmp(ctx, scope, targets); err != nil {
		resp.Diagnostics.AddError("Branch "+branchName+" did not become ready", err.Error())
		return
	}
	// AMP refuses extensions on a new branch, so they are added once it exists.
	if extensions := stringsFromTf(plan.Extensions); len(extensions) > 0 {
		items := []ampPatchItem{{Kind: ampKindBranch, Name: branchPath, Spec: ampBranchSpec{Extensions: extensions}}}
		if err := scope.apply(ctx, region, ampIntentUpdate, items); err != nil {
			resp.Diagnostics.AddError("Unable to add extensions to branch "+branchName, err.Error())
			return
		}
		if err := waitForAmp(ctx, scope, targets); err != nil {
			resp.Diagnostics.AddError("Branch "+branchName+" did not converge", err.Error())
			return
		}
	}

	plan.ParentBranch = types.String{Value: parent}
	branch, _, err := resourceAmpBranchRead(ctx, scope, clusterName, branchName, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of branch "+branchName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &branch)...)
}

func resourceAmpBranchRead(ctx context.Context, scope ampScope, clusterName, branchName string, prior AmpBranch) (branch AmpBranch, found bool, err error) {
	snap, err := scope.snapshot(ctx)
	if err != nil {
		return branch, false, err
	}
	pool := scope.pool()
	branchPath := ampBranchName(pool, clusterName, branchName)
	env, ok := ampEnvDeclaring(snap, ampKindBranch, branchPath)
	if !ok {
		return branch, false, nil
	}
	res, _ := findAmpDesired(env.Desired, ampKindBranch, branchPath)
	var spec ampBranchSpec
	if len(res.Spec) > 0 {
		if err := json.Unmarshal(res.Spec, &spec); err != nil {
			return branch, true, fmt.Errorf("unable to parse the spec of branch %s: %v", branchName, err)
		}
	}
	var pg map[string]string
	if spec.PgSettings != nil {
		pg = *spec.PgSettings
	}
	branch = AmpBranch{
		AccountID:     types.String{Value: scope.accountId},
		EnvironmentID: optionalString(prior.EnvironmentID),
		ClusterName:   types.String{Value: clusterName},
		BranchName:    types.String{Value: branchName},
		ParentBranch:  optionalString(prior.ParentBranch),
		ForkPointLsn:  tfString(spec.ForkPointLsn),
		PgSettings:    mapToTf(pg),
		Extensions:    stringsToTf(spec.Extensions),
		State:         types.String{Value: "creating"},
	}
	if o, ok := findAmpObserved(env.Observed, ampKindBranch, branchPath); ok {
		branch.BranchID = tfString(o.Attrs["id"])
		branch.State = tfString(o.State)
	} else {
		branch.BranchID = types.String{Null: true}
	}
	// An import knows nothing of the parent but its ID.
	if branch.ParentBranch.Null {
		branch.ParentBranch = types.String{Value: ampDefaultBranch}
		if spec.ParentBranchID != "" {
			if name := ampBranchNameByID(env.Observed, ampClusterName(pool, clusterName), spec.ParentBranchID); name != "" {
				branch.ParentBranch = types.String{Value: name}
			}
		}
	}
	return branch, true, nil
}

func (r resourceAmpBranch) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state AmpBranch
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
	branch, found, err := resourceAmpBranchRead(ctx, scope, state.ClusterName.Value, state.BranchName.Value, state)
	if isYcpNotFound(err) || (err == nil && !found) {
		tflog.Warn(ctx, "Branch "+state.BranchName.Value+" no longer exists; removing it from state")
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of branch "+state.BranchName.Value, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &branch)...)
}

func (r resourceAmpBranch) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan, state AmpBranch
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	clusterName, branchName := state.ClusterName.Value, state.BranchName.Value

	extensions := stringsFromTf(plan.Extensions)
	for _, old := range stringsFromTf(state.Extensions) {
		if !containsString(extensions, old) {
			resp.Diagnostics.AddError("Unable to update branch "+branchName,
				fmt.Sprintf("Extension %s cannot be removed from a branch; extensions can only be added.", old))
			return
		}
	}

	scope, region, err := ampChildScope(ctx, r.p, state.EnvironmentID, clusterName)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update branch "+branchName, err.Error())
		return
	}
	branchPath := ampBranchName(scope.pool(), clusterName, branchName)
	// A declared pg_settings map owns the whole map, so an emptied one is sent to clear it.
	pg := mapFromTf(plan.PgSettings)
	items := []ampPatchItem{{Kind: ampKindBranch, Name: branchPath, Spec: ampBranchSpec{PgSettings: &pg, Extensions: extensions}}}
	if err := scope.apply(ctx, region, ampIntentUpdate, items); err != nil {
		resp.Diagnostics.AddError("Unable to update branch "+branchName, err.Error())
		return
	}
	if err := waitForAmp(ctx, scope, []ampTarget{{Kind: ampKindBranch, Name: branchPath}}); err != nil {
		resp.Diagnostics.AddError("Branch "+branchName+" did not converge", err.Error())
		return
	}
	branch, _, err := resourceAmpBranchRead(ctx, scope, clusterName, branchName, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of branch "+branchName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &branch)...)
}

func containsString(values []string, v string) bool {
	for _, s := range values {
		if s == v {
			return true
		}
	}
	return false
}

func (r resourceAmpBranch) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state AmpBranch
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope := newAmpScope(r.p.client, state.AccountID.Value, state.EnvironmentID.Value)
	clusterName, branchName := state.ClusterName.Value, state.BranchName.Value
	region, err := scope.clusterRegion(ctx, clusterName)
	if isYcpNotFound(err) {
		// Deleting the cluster already took the branch with it.
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete branch "+branchName, err.Error())
		return
	}
	branchPath := ampBranchName(scope.pool(), clusterName, branchName)
	// Deleting the branch cascades to its endpoints.
	if err := scope.apply(ctx, region, ampIntentDelete, []ampPatchItem{{Kind: ampKindBranch, Name: branchPath}}); err != nil && !isYcpNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete branch "+branchName, err.Error())
		return
	}
	if err := waitForAmpGone(ctx, scope, ampKindBranch, branchPath); err != nil {
		resp.Diagnostics.AddError("Unable to delete branch "+branchName, "The operation timed out waiting for the branch deletion to complete.")
		return
	}
	resp.State.RemoveResource(ctx)
}

// ImportState imports a branch by "<cluster_name>/<branch_name>", or
// "<environment_id>/<cluster_name>/<branch_name>" for a BYOC cluster.
func (r resourceAmpBranch) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) == 3 {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("environment_id"), parts[0])...)
		parts = parts[1:]
	}
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <cluster_name>/<branch_name> or <environment_id>/<cluster_name>/<branch_name>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("cluster_name"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("branch_name"), parts[1])...)
}
