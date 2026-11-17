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

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *resourceAmpCluster) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := ampClusterAttributes()
	attributes["region"] = schema.StringAttribute{
		Description:   "The region to create the cluster in, for example us-east-1. Changing it forces a new cluster.",
		Required:      true,
		PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}
	attributes["environment_id"] = schema.StringAttribute{
		Description:   "The ID of the environment hosting the cluster.",
		Computed:      true,
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseNonNullStateForUnknown()},
	}
	resp.Schema = schema.Schema{
		Description: `The resource to create a shared PostgreSQL cluster in YugabyteDB Aeon. ` +
			`The cluster is created in the account's storage pool with its main branch. ` +
			`Connect to it through a ybm_amp_endpoint on main with role read_write, and add branches with ybm_amp_branch.`,
		Attributes: attributes,
	}
}

func newResourceAmpCluster() resource.Resource {
	return &resourceAmpCluster{}
}

func (r *resourceAmpCluster) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_amp_cluster"
}

func (r *resourceAmpCluster) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type resourceAmpCluster struct {
	p provider
}

func ampClusterConfigFromPlan(plan AmpCluster, prior *AmpCluster) ampClusterConfig {
	priorPg := types.MapNull(types.StringType)
	if prior != nil {
		priorPg = prior.BranchPgSettings
	}
	return newAmpClusterConfig(plan.PgVersion, plan.Engine, plan.Extensions, plan.BranchPgSettings, plan.BranchExtensions, priorPg)
}

func (r resourceAmpCluster) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	modifyAmpClusterPlan(ctx, req, resp)
}

func (r resourceAmpCluster) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
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
	scope := newAmpScope(r.p.client, accountId, "", "")
	clusterName := plan.ClusterName.ValueString()
	created, createErr := createAmpCluster(ctx, scope, plan.Region.ValueString(), clusterName, ampClusterConfigFromPlan(plan, nil))
	if !created {
		resp.Diagnostics.AddError("Unable to create cluster "+clusterName, createErr.Error())
		return
	}
	// The cluster exists from here on, so its state is saved even when the create did not finish:
	// Terraform then marks it tainted rather than losing track of it.
	cluster, found, err := resourceAmpClusterRead(ctx, scope, clusterName, plan)
	err = requireAmpFound(found, err, "cluster "+clusterName)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of cluster "+clusterName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cluster)...)
	if createErr != nil {
		resp.Diagnostics.AddError("Unable to create cluster "+clusterName, createErr.Error())
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
	cluster = AmpCluster{
		AccountID:        types.StringValue(scope.accountId),
		ClusterName:      types.StringValue(clusterName),
		Region:           types.StringValue(env.Region),
		PgVersion:        tfInt64(status.Config.PgVersion),
		Engine:           tfString(status.Config.Engine),
		Extensions:       stringsToTf(status.Config.Extensions, prior.Extensions),
		BranchPgSettings: mapToTf(derefMap(status.Config.BranchPgSettings), prior.BranchPgSettings),
		BranchExtensions: stringsToTf(status.Config.BranchExtensions, prior.BranchExtensions),
		ForceDestroy:     prior.ForceDestroy,
		ClusterID:        tfString(status.ClusterID),
		EnvironmentID:    tfString(env.EnvironmentID),
		State:            tfString(status.State),
	}
	return cluster, true, nil
}

func (r resourceAmpCluster) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AmpCluster
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

	cluster, found, err := resourceAmpClusterRead(ctx, newAmpScope(r.p.client, accountId, "", ""), state.ClusterName.ValueString(), state)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of cluster "+state.ClusterName.ValueString(), err.Error())
		return
	}
	if !found {
		tflog.Warn(ctx, "Cluster "+state.ClusterName.ValueString()+" no longer exists; removing it from state")
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cluster)...)
}

func (r resourceAmpCluster) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state AmpCluster
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope := newAmpScope(r.p.client, state.AccountID.ValueString(), "", "")
	clusterName := state.ClusterName.ValueString()

	if err := updateAmpCluster(ctx, scope, state.Region.ValueString(), clusterName, ampClusterConfigFromPlan(plan, &state)); err != nil {
		resp.Diagnostics.AddError("Unable to update cluster "+clusterName, err.Error())
		return
	}
	if err := waitForAmp(ctx, scope, ampClusterTargets(scope.pool(), clusterName)); err != nil {
		resp.Diagnostics.AddError("Cluster "+clusterName+" did not converge", err.Error())
		return
	}
	cluster, found, err := resourceAmpClusterRead(ctx, scope, clusterName, plan)
	err = requireAmpFound(found, err, "cluster "+clusterName)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the state of cluster "+clusterName, err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cluster)...)
}

func (r resourceAmpCluster) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AmpCluster
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	scope := newAmpScope(r.p.client, state.AccountID.ValueString(), "", "")
	clusterName := state.ClusterName.ValueString()
	if err := deleteAmpCluster(ctx, scope, state.Region.ValueString(), clusterName, boolValue(state.ForceDestroy)); err != nil {
		resp.Diagnostics.AddError("Unable to delete cluster "+clusterName, err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

// ImportState imports a cluster by its name.
func (r resourceAmpCluster) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("cluster_name"), req, resp)
}
