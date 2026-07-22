/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceAutoscalerPolicyType struct{}

func (r resourceAutoscalerPolicyType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to manage an autoscaler policy for a YugabyteDB Aeon cluster.
Requires the AUTOSCALING feature flag (YBM_FF_AUTOSCALING=true).`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this autoscaler policy belongs to.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"project_id": {
				Description: "The ID of the project this autoscaler policy belongs to.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"cluster_id": {
				Description: "The ID of the cluster this autoscaler policy belongs to.",
				Type:        types.StringType,
				Required:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.RequiresReplace(),
				},
			},
			"policy_id": {
				Description: "The ID of the autoscaler policy.",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"status": {
				Description: "The status of the autoscaler policy (ACTIVE or INACTIVE).",
				Type:        types.StringType,
				Computed:    true,
			},
			"clusters": {
				Description: "Cluster-level autoscaler policy configuration (PRIMARY and/or READ_REPLICA).",
				Required:    true,
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
					"cluster_id": {
						Description: "The ID of the cluster (PRIMARY or READ_REPLICA) this policy applies to.",
						Type:        types.StringType,
						Required:    true,
					},
					"type": {
						Description: "Cluster type: PRIMARY or READ_REPLICA.",
						Type:        types.StringType,
						Required:    true,
						Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("PRIMARY", "READ_REPLICA")},
					},
					"regions": {
						Description: "Region-level autoscaler policy configuration.",
						Required:    true,
						Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
							"code": {
								Description: "Cloud region code (for example, us-west1).",
								Type:        types.StringType,
								Required:    true,
							},
							"policies": {
								Description: "Scaling policies for the region.",
								Required:    true,
								Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
									"scalable_resource": {
										Description: "Resource type that can be scaled (for example, NODE).",
										Type:        types.StringType,
										Required:    true,
									},
									"min": {
										Description: "Minimum number of scalable resources.",
										Type:        types.Int64Type,
										Required:    true,
									},
									"max": {
										Description: "Maximum number of scalable resources.",
										Type:        types.Int64Type,
										Required:    true,
									},
									"scaling_type": {
										Description: "Direction of scaling: SCALE_IN or SCALE_OUT.",
										Type:        types.StringType,
										Required:    true,
										Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("SCALE_IN", "SCALE_OUT")},
									},
									"clause": {
										Description: "Logical operator for combining scaling rules: AND or OR.",
										Type:        types.StringType,
										Required:    true,
										Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("AND", "OR")},
									},
									"rules": {
										Description: "Rules that must be satisfied to trigger scaling.",
										Required:    true,
										Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
											"resource": {
												Description: "Metric resource to evaluate: CPU or SQL_CONNECTION.",
												Type:        types.StringType,
												Required:    true,
												Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("CPU", "SQL_CONNECTION")},
											},
											"condition": {
												Description: "Comparison operator for the metric threshold: GT or LT.",
												Type:        types.StringType,
												Required:    true,
												Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("GT", "LT")},
											},
											"value": {
												Description: "Threshold value for the metric.",
												Type:        types.Float64Type,
												Required:    true,
											},
											"evaluation_window": {
												Description: "Duration over which the metric is evaluated (for example, 5m).",
												Type:        types.StringType,
												Required:    true,
											},
										}),
									},
									"scaling_action": {
										Description: "Scaling action to apply when rules are satisfied.",
										Required:    true,
										Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
											"delta": {
												Description: "Number of nodes to scale in or out.",
												Type:        types.Int64Type,
												Required:    true,
											},
										}),
									},
								}),
							},
						}),
					},
				}),
			},
		},
	}, nil
}

func (r resourceAutoscalerPolicyType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceAutoscalerPolicy{
		p: *(p.(*provider)),
	}, nil
}

type resourceAutoscalerPolicy struct {
	p provider
}

func getAutoscalerPolicyPlan(ctx context.Context, plan tfsdk.Plan, policy *AutoscalerPolicy) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &policy.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("clusters"), &policy.Clusters)...)
	return diags
}

func getAutoscalerPolicyState(ctx context.Context, state tfsdk.State, policy *AutoscalerPolicy) {
	state.GetAttribute(ctx, path.Root("account_id"), &policy.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &policy.ProjectID)
	state.GetAttribute(ctx, path.Root("cluster_id"), &policy.ClusterID)
	state.GetAttribute(ctx, path.Root("policy_id"), &policy.PolicyID)
	state.GetAttribute(ctx, path.Root("status"), &policy.Status)
	state.GetAttribute(ctx, path.Root("clusters"), &policy.Clusters)
}

func buildCreateAutoscalerPolicyRequestSpec(clusters []AutoscalerPolicyCluster) openapiclient.CreateAutoscalerPolicyRequestSpec {
	clusterSpecs := make([]openapiclient.AutoscalerClusterSpec, 0, len(clusters))
	for _, cluster := range clusters {
		regionSpecs := make([]openapiclient.AutoscalerClusterRegionSpec, 0, len(cluster.Regions))
		for _, region := range cluster.Regions {
			policySpecs := make([]openapiclient.AutoscalerClusterRegionPolicySpec, 0, len(region.Policies))
			for _, policy := range region.Policies {
				rules := make([]openapiclient.AutoscalerClusterRegionPolicyScalingRuleSpec, 0, len(policy.Rules))
				for _, rule := range policy.Rules {
					ruleSpec := openapiclient.NewAutoscalerClusterRegionPolicyScalingRuleSpec(
						rule.Resource.Value,
						rule.Condition.Value,
						rule.Value.Value,
						rule.EvaluationWindow.Value,
					)
					rules = append(rules, *ruleSpec)
				}

				action := openapiclient.NewAutoscalerClusterRegionPolicyScalingActionSpec(int32(policy.ScalingAction.Delta.Value))
				policySpec := openapiclient.NewAutoscalerClusterRegionPolicySpec(
					policy.ScalableResource.Value,
					int32(policy.Min.Value),
					int32(policy.Max.Value),
					policy.ScalingType.Value,
					policy.Clause.Value,
					rules,
					*action,
				)
				policySpecs = append(policySpecs, *policySpec)
			}

			regionSpec := openapiclient.NewAutoscalerClusterRegionSpec(region.Code.Value, policySpecs)
			regionSpecs = append(regionSpecs, *regionSpec)
		}

		clusterSpec := openapiclient.NewAutoscalerClusterSpec(
			cluster.ClusterID.Value,
			cluster.Type.Value,
			regionSpecs,
		)
		clusterSpecs = append(clusterSpecs, *clusterSpec)
	}

	return *openapiclient.NewCreateAutoscalerPolicyRequestSpec(clusterSpecs)
}

func mapAutoscalerPolicyFromResponse(
	accountId string,
	projectId string,
	clusterId string,
	response openapiclient.AutoscalerPolicyResponse,
) AutoscalerPolicy {
	data := response.GetData()
	metadata := data.GetMetadata()
	policy := AutoscalerPolicy{
		AccountID: types.String{Value: accountId},
		ProjectID: types.String{Value: projectId},
		ClusterID: types.String{Value: clusterId},
		PolicyID:  types.String{Value: metadata.GetId()},
		Status:    types.String{Value: data.GetStatus()},
		Clusters:  []AutoscalerPolicyCluster{},
	}

	for _, cluster := range data.GetClusters() {
		tfCluster := AutoscalerPolicyCluster{
			ClusterID: types.String{Value: cluster.GetClusterId()},
			Type:      types.String{Value: cluster.GetType()},
			Regions:   []AutoscalerPolicyClusterRegion{},
		}

		for _, region := range cluster.GetRegions() {
			tfRegion := AutoscalerPolicyClusterRegion{
				Code:     types.String{Value: region.GetCode()},
				Policies: []AutoscalerClusterRegionScalingPolicy{},
			}

			for _, regionPolicy := range region.GetPolicies() {
				tfRules := make([]AutoscalerScalingRule, 0, len(regionPolicy.GetRules()))
				for _, rule := range regionPolicy.GetRules() {
					tfRules = append(tfRules, AutoscalerScalingRule{
						Resource:         types.String{Value: rule.GetResource()},
						Condition:        types.String{Value: rule.GetCondition()},
						Value:            types.Float64{Value: rule.GetValue()},
						EvaluationWindow: types.String{Value: rule.GetEvaluationWindow()},
					})
				}

				scalingAction := regionPolicy.GetScalingAction()
				tfPolicy := AutoscalerClusterRegionScalingPolicy{
					ScalableResource: types.String{Value: regionPolicy.GetScalableResource()},
					Min:              types.Int64{Value: int64(regionPolicy.GetMin())},
					Max:              types.Int64{Value: int64(regionPolicy.GetMax())},
					ScalingType:      types.String{Value: regionPolicy.GetScalingType()},
					Clause:           types.String{Value: regionPolicy.GetClause()},
					Rules:            tfRules,
					ScalingAction: &AutoscalerScalingAction{
						Delta: types.Int64{Value: int64(scalingAction.GetDelta())},
					},
				}
				tfRegion.Policies = append(tfRegion.Policies, tfPolicy)
			}

			tfCluster.Regions = append(tfCluster.Regions, tfRegion)
		}

		policy.Clusters = append(policy.Clusters, tfCluster)
	}

	return policy
}

func resourceAutoscalerPolicyRead(
	accountId string,
	projectId string,
	clusterId string,
	apiClient *openapiclient.APIClient,
) (AutoscalerPolicy, bool, string) {
	resp, response, err := apiClient.AutoscalerApi.ListAutoscalerPolicies(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return AutoscalerPolicy{}, false, "Delete resource"
		}
		return AutoscalerPolicy{}, false, getErrorMessage(response, err)
	}

	return mapAutoscalerPolicyFromResponse(accountId, projectId, clusterId, resp), true, ""
}

func (r resourceAutoscalerPolicy) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan AutoscalerPolicy
	resp.Diagnostics.Append(getAutoscalerPolicyPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the autoscaler policy")
		return
	}

	apiClient := r.p.client

	accountId, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID", message)
		return
	}

	clusterId := plan.ClusterID.Value
	requestSpec := buildCreateAutoscalerPolicyRequestSpec(plan.Clusters)

	_, response, err := apiClient.AutoscalerApi.CreateAutoscalerPolicy(ctx, accountId, projectId, clusterId).
		CreateAutoscalerPolicyRequestSpec(requestSpec).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to create autoscaler policy", getErrorMessage(response, err))
		return
	}

	policy, readOK, message := resourceAutoscalerPolicyRead(accountId, projectId, clusterId, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the autoscaler policy after creation", message)
		return
	}

	tflog.Debug(ctx, "Autoscaler policy created", map[string]interface{}{"policy": policy})

	diags := resp.State.Set(ctx, &policy)
	resp.Diagnostics.Append(diags...)
}

func (r resourceAutoscalerPolicy) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state AutoscalerPolicy
	getAutoscalerPolicyState(ctx, req.State, &state)

	policy, readOK, message := resourceAutoscalerPolicyRead(
		state.AccountID.Value,
		state.ProjectID.Value,
		state.ClusterID.Value,
		r.p.client,
	)
	if !readOK {
		if message == "Delete resource" {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the state of the autoscaler policy", message)
		return
	}

	diags := resp.State.Set(ctx, &policy)
	resp.Diagnostics.Append(diags...)
}

func (r resourceAutoscalerPolicy) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan AutoscalerPolicy
	var state AutoscalerPolicy

	getAutoscalerPolicyState(ctx, req.State, &state)
	resp.Diagnostics.Append(getAutoscalerPolicyPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the autoscaler policy update")
		return
	}

	if plan.ClusterID.Value != state.ClusterID.Value {
		resp.Diagnostics.AddError(
			"Invalid edit to autoscaler policy",
			"cluster_id cannot be changed. Destroy and recreate the resource to use a different cluster.",
		)
		return
	}

	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value
	requestSpec := buildCreateAutoscalerPolicyRequestSpec(plan.Clusters)

	_, response, err := r.p.client.AutoscalerApi.UpdateAutoscalerPolicy(ctx, accountId, projectId, clusterId).
		CreateAutoscalerPolicyRequestSpec(requestSpec).
		Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to update autoscaler policy", getErrorMessage(response, err))
		return
	}

	policy, readOK, message := resourceAutoscalerPolicyRead(accountId, projectId, clusterId, r.p.client)
	if !readOK {
		if message == "Delete resource" {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the state of the autoscaler policy after update", message)
		return
	}

	diags := resp.State.Set(ctx, &policy)
	resp.Diagnostics.Append(diags...)
}

func (r resourceAutoscalerPolicy) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state AutoscalerPolicy
	getAutoscalerPolicyState(ctx, req.State, &state)

	accountId := state.AccountID.Value
	projectId := state.ProjectID.Value
	clusterId := state.ClusterID.Value

	response, err := r.p.client.AutoscalerApi.DeleteAutoscalerPolicy(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to delete autoscaler policy", getErrorMessage(response, err))
		return
	}

	resp.State.RemoveResource(ctx)
}

func (r resourceAutoscalerPolicy) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Import identifier is the cluster ID.
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("cluster_id"), req, resp)
}
