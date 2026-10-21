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
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type resourceByocStoragePoolType struct{}

func (r resourceByocStoragePoolType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to create a storage pool in a BYOC environment: a group of clusters, for example for a team or business unit. ` +
			`Create ybm_aeon_pg_byoc_cluster resources in it with storage_pool. Storage pools cannot be deleted, so destroying this resource ` +
			`only removes it from Terraform state; import the pool to manage it again.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description:   "The ID of the account the environment belongs to.",
				Type:          types.StringType,
				Computed:      true,
				PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
			},
			"environment_id": {
				Description:   "The ID of the BYOC environment to create the storage pool in. Changing it forces a new storage pool.",
				Type:          types.StringType,
				Required:      true,
				PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.RequiresReplace()},
			},
			"name": ampNameAttribute("The name of the storage pool, unique within the environment. Changing it forces a new storage pool."),
			"owner_user_id": {
				Description:   "The UUID of the pool's owner, which the environment's agent creates. Defaults to the account ID. Changing it forces a new storage pool.",
				Type:          types.StringType,
				Optional:      true,
				Computed:      true,
				PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown(), tfsdk.RequiresReplace()},
			},
			"state": {
				Description: "The state the storage pool last reported.",
				Type:        types.StringType,
				Computed:    true,
			},
		},
	}, nil
}

func (r resourceByocStoragePoolType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceByocStoragePool{
		p: *(p.(*provider)),
	}, nil
}

type resourceByocStoragePool struct {
	p provider
}

func (r resourceByocStoragePool) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}
	var plan ByocStoragePool
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	accountId, getAccountOK, message := getAccountId(ctx, r.p.client)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}
	environmentId, name := plan.EnvironmentID.Value, plan.Name.Value
	owner := stringOr(plan.OwnerUserID, accountId)
	if err := waitForActiveByocEnvironment(ctx, r.p.client, accountId, environmentId); err != nil {
		resp.Diagnostics.AddError("Environment "+environmentId+" cannot run clusters", err.Error())
		return
	}

	err := updateByocDesired(ctx, r.p.client, accountId, environmentId, func(desired []ampResource) ([]ampResource, error) {
		if _, ok := findAmpDesired(desired, ampKindStoragePool, name); ok {
			return nil, &ycpError{StatusCode: http.StatusConflict, Message: fmt.Sprintf(
				"storage pool %s already exists in environment %s; import it with terraform import", name, environmentId)}
		}
		return applyAmpIntent(desired, ampIntentCreate, []ampPatchItem{{Kind: ampKindStoragePool, Name: name, Spec: ampStoragePoolSpec{OwnerUserID: owner}}})
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create storage pool "+name, err.Error())
		return
	}
	scope := newAmpScope(r.p.client, accountId, environmentId, name)
	waitErr := waitForAmp(ctx, scope, []ampTarget{{Kind: ampKindStoragePool, Name: name}})
	pool, _, err := resourceByocStoragePoolRead(ctx, scope)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of storage pool "+name, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &pool)...)
	if waitErr != nil {
		resp.Diagnostics.AddError("Storage pool "+name+" did not become ready", waitErr.Error())
	}
}

func resourceByocStoragePoolRead(ctx context.Context, scope ampScope) (pool ByocStoragePool, found bool, err error) {
	snap, err := scope.snapshot(ctx)
	if err != nil {
		return pool, false, err
	}
	res, ok := findAmpDesired(snap[0].Desired, ampKindStoragePool, scope.pool())
	if !ok {
		return pool, false, nil
	}
	var spec ampStoragePoolSpec
	if len(res.Spec) > 0 {
		if err := json.Unmarshal(res.Spec, &spec); err != nil {
			return pool, true, fmt.Errorf("unable to parse the spec of storage pool %s: %v", scope.pool(), err)
		}
	}
	pool = ByocStoragePool{
		AccountID:     types.String{Value: scope.accountId},
		EnvironmentID: types.String{Value: scope.environmentId},
		Name:          types.String{Value: scope.pool()},
		OwnerUserID:   tfString(spec.OwnerUserID),
		State:         types.String{Value: "creating"},
	}
	if o, ok := findAmpObserved(snap[0].Observed, ampKindStoragePool, scope.pool()); ok {
		pool.State = tfString(o.State)
	}
	return pool, true, nil
}

func (r resourceByocStoragePool) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state ByocStoragePool
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
	scope := newAmpScope(r.p.client, accountId, state.EnvironmentID.Value, state.Name.Value)
	pool, found, err := resourceByocStoragePoolRead(ctx, scope)
	if isYcpNotFound(err) || (err == nil && !found) {
		tflog.Warn(ctx, "Storage pool "+state.Name.Value+" no longer exists; removing it from state")
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of storage pool "+state.Name.Value, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &pool)...)
}

// Update only refreshes computed attributes: every configurable attribute forces a replacement.
func (r resourceByocStoragePool) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var state ByocStoragePool
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope := newAmpScope(r.p.client, state.AccountID.Value, state.EnvironmentID.Value, state.Name.Value)
	pool, _, err := resourceByocStoragePoolRead(ctx, scope)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of storage pool "+state.Name.Value, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &pool)...)
}

// Delete leaves the pool in the environment: AMP does not delete storage pools, and the console
// treats them as undeletable too.
func (r resourceByocStoragePool) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state ByocStoragePool
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning("Storage pool "+state.Name.Value+" was not deleted",
		"Storage pools cannot be deleted, so the pool stays in environment "+state.EnvironmentID.Value+
			" and has only been removed from Terraform state. Import it to manage it again.")
	resp.State.RemoveResource(ctx)
}

// ImportState imports a storage pool by "<environment_id>/<name>".
func (r resourceByocStoragePool) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Expected <environment_id>/<name>, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("environment_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
}
