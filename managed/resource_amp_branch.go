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
		"storage_pool": ampStoragePoolAttribute("The storage pool of the BYOC cluster. Required with environment_id, and not allowed without it. Changing it forces a replacement.", false),
		"cluster_name": ampNameAttribute("The name of the cluster. Changing it forces a replacement."),
	}
}

// checkAmpParentPlan returns the errors in where a planned branch or endpoint places itself.
func checkAmpParentPlan(ctx context.Context, plan tfsdk.Plan) diag.Diagnostics {
	var diags diag.Diagnostics
	var environmentId, storagePool types.String
	diags.Append(plan.GetAttribute(ctx, path.Root("environment_id"), &environmentId)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("storage_pool"), &storagePool)...)
	if diags.HasError() || environmentId.Unknown || storagePool.Unknown {
		return diags
	}
	if !environmentId.Null && storagePool.Null {
		diags.AddAttributeError(path.Root("storage_pool"), "Missing storage pool", "A BYOC cluster's branches and endpoints need its storage_pool.")
	}
	if environmentId.Null && !storagePool.Null {
		diags.AddAttributeError(path.Root("storage_pool"), "Unexpected storage pool",
			"A shared cluster is always in the account's storage pool; set storage_pool only with environment_id.")
	}
	return diags
}

// ampChildScope returns the scope of the cluster a branch or endpoint belongs to, and the region
// to write it in. It waits for a BYOC environment's agent to enroll.
func ampChildScope(ctx context.Context, p provider, environmentId types.String, storagePool types.String, clusterName string) (ampScope, string, error) {
	accountId, ok, message := getAccountId(ctx, p.client)
	if !ok {
		return ampScope{}, "", fmt.Errorf("unable to get account ID: %s", message)
	}
	scope := newAmpScope(p.client, accountId, stringOr(environmentId, ""), stringOr(storagePool, ""))
	if scope.isByoc() {
		if err := waitForActiveByocEnvironment(ctx, p.client, accountId, scope.environmentId); err != nil {
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
	attributes["branch_name"] = ampNameAttribute("The name of the branch. The default branch, main, is created with the cluster and cannot be managed here; set its settings with the cluster's branch_pg_settings. Changing it forces a new branch.")
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
	attributes["pg_settings"] = ampPgSettingsAttribute("PostgreSQL settings of the branch, such as work_mem. They apply to every endpoint on the branch; " +
		"an endpoint's own pg_settings override them.")
	attributes["extensions"] = tfsdk.Attribute{
		Description: "Extensions to install in the branch. Extensions can be added in place but not removed.",
		Type:        types.ListType{ElemType: types.StringType},
		Optional:    true,
	}
	attributes["ttl"] = tfsdk.Attribute{
		Description: "Reserved for deleting the branch automatically after a duration, such as 24h. Not supported yet.",
		Type:        types.StringType,
		Optional:    true,
	}
	attributes["force_destroy"] = ampForceDestroyAttribute(
		"Whether destroying or replacing the branch may also delete its endpoints and the branches forked from it. " +
			"Without it, deleting a branch that still has them fails.")
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

func (r resourceAmpBranch) ModifyPlan(ctx context.Context, req tfsdk.ModifyResourcePlanRequest, resp *tfsdk.ModifyResourcePlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(checkAmpParentPlan(ctx, req.Plan)...)
	var ttl types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("ttl"), &ttl)...)
	if known(ttl.Null, ttl.Unknown) {
		resp.Diagnostics.AddAttributeError(path.Root("ttl"), "Not supported yet", "AMP branches cannot expire yet. Remove ttl.")
	}
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
		resp.Diagnostics.AddError("Invalid branch name", "The main branch is created with the cluster and cannot be managed as a ybm_amp_branch; set its settings with the cluster's branch_pg_settings.")
		return
	}
	scope, region, err := ampChildScope(ctx, r.p, plan.EnvironmentID, plan.StoragePool, clusterName)
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
	createErr := func() error {
		if err := waitForAmp(ctx, scope, targets); err != nil {
			return fmt.Errorf("the branch did not become ready: %v", err)
		}
		// AMP refuses extensions on a new branch, so they are added once it exists.
		if extensions := stringsFromTf(plan.Extensions); len(extensions) > 0 {
			items := []ampPatchItem{{Kind: ampKindBranch, Name: branchPath, Spec: ampBranchSpec{Extensions: extensions}}}
			if err := scope.apply(ctx, region, ampIntentUpdate, items); err != nil {
				return fmt.Errorf("unable to add extensions: %v", err)
			}
			if err := waitForAmp(ctx, scope, targets); err != nil {
				return fmt.Errorf("the extensions were not added: %v", err)
			}
		}
		return nil
	}()

	// The branch exists from here on, so its state is saved even when the create did not finish:
	// Terraform then marks it tainted rather than losing track of it.
	plan.ParentBranch = types.String{Value: parent}
	branch, _, err := resourceAmpBranchRead(ctx, scope, clusterName, branchName, plan)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of branch "+branchName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &branch)...)
	if createErr != nil {
		resp.Diagnostics.AddError("Unable to create branch "+branchName, createErr.Error())
	}
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
		StoragePool:   optionalString(prior.StoragePool),
		ClusterName:   types.String{Value: clusterName},
		BranchName:    types.String{Value: branchName},
		ForkPointLsn:  tfString(spec.ForkPointLsn),
		PgSettings:    mapToTf(pg, prior.PgSettings),
		Extensions:    stringsToTf(spec.Extensions, prior.Extensions),
		Ttl:           types.String{Null: true},
		ForceDestroy:  prior.ForceDestroy,
		State:         types.String{Value: "creating"},
	}
	if o, ok := findAmpObserved(env.Observed, ampKindBranch, branchPath); ok {
		branch.BranchID = tfString(o.Attrs["id"])
		branch.State = tfString(o.State)
	} else {
		branch.BranchID = types.String{Null: true}
	}
	// The parent comes from the spec, which names it by ID. Until the agent reports the parent,
	// its name is not known, so the prior one is kept.
	branch.ParentBranch = types.String{Value: ampDefaultBranch}
	if spec.ParentBranchID != "" {
		if name := ampBranchNameByID(env.Observed, ampClusterName(pool, clusterName), spec.ParentBranchID); name != "" {
			branch.ParentBranch = types.String{Value: name}
		} else if p := optionalString(prior.ParentBranch); !p.Null {
			branch.ParentBranch = p
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
	scope := newAmpScope(r.p.client, accountId, state.EnvironmentID.Value, state.StoragePool.Value)
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

	scope, region, err := ampChildScope(ctx, r.p, state.EnvironmentID, state.StoragePool, clusterName)
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
	scope := newAmpScope(r.p.client, state.AccountID.Value, state.EnvironmentID.Value, state.StoragePool.Value)
	clusterName, branchName := state.ClusterName.Value, state.BranchName.Value
	err := deleteAmpBranch(ctx, scope, clusterName, branchName, boolValue(state.ForceDestroy))
	if err != nil && !isYcpNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete branch "+branchName, err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

// deleteAmpBranch deletes a branch with its endpoints and the branches forked from it. Unless
// forced, it refuses while the branch has any, so Terraform state never keeps resources the
// deletion took with it.
func deleteAmpBranch(ctx context.Context, scope ampScope, clusterName, branchName string, force bool) error {
	clusterPath := ampClusterName(scope.pool(), clusterName)
	branchPath := ampBranchName(scope.pool(), clusterName, branchName)
	snap, err := scope.snapshot(ctx)
	if err != nil {
		return err
	}
	env, ok := ampEnvDeclaring(snap, ampKindBranch, branchPath)
	if !ok {
		// Deleting the cluster already took the branch with it.
		return nil
	}
	roots := append([]string{branchPath}, ampForks(env, clusterPath, branchPath)...)
	if !force {
		if dependents := ampDependents(env, roots, map[string]bool{ampKindBranch + " " + branchPath: true}); len(dependents) > 0 {
			return fmt.Errorf("branch %s still has %s. Delete them first, or set force_destroy = true and apply "+
				"before destroying the branch to delete them with it", branchName, strings.Join(dependents, ", "))
		}
	}
	items := make([]ampPatchItem, 0, len(roots))
	targets := make([]ampTarget, 0, len(roots))
	for _, root := range roots {
		items = append(items, ampPatchItem{Kind: ampKindBranch, Name: root})
		targets = append(targets, ampTarget{Kind: ampKindBranch, Name: root})
	}
	if err := scope.apply(ctx, env.Region, ampIntentDelete, items); err != nil && !isYcpNotFound(err) {
		return err
	}
	return waitForAmpGone(ctx, scope, targets)
}

// ImportState imports a branch by "<cluster_name>/<branch_name>", or
// "<environment_id>/<storage_pool>/<cluster_name>/<branch_name>" for a BYOC cluster.
func (r resourceAmpBranch) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) == 4 && parts[0] != "" && parts[1] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("environment_id"), parts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("storage_pool"), parts[1])...)
		parts = parts[2:]
	}
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <cluster_name>/<branch_name> or <environment_id>/<storage_pool>/<cluster_name>/<branch_name>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("cluster_name"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("branch_name"), parts[1])...)
}
