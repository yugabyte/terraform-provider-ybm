/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

type resourceAutoscalerPolicyType struct{}

func autoscalerMetadataSchemaAttributes() map[string]tfsdk.Attribute {
	return map[string]tfsdk.Attribute{
		"id": {
			Description: "Entity ID.",
			Type:        types.StringType,
			Computed:    true,
		},
		"created_at": {
			Description: "Timestamp when the entity was created (UTC).",
			Type:        types.StringType,
			Computed:    true,
		},
		"updated_at": {
			Description: "Timestamp when the entity was last updated (UTC).",
			Type:        types.StringType,
			Computed:    true,
		},
	}
}

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
			"created_at": {
				Description: "Timestamp when the autoscaler policy was created (UTC).",
				Type:        types.StringType,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"updated_at": {
				Description: "Timestamp when the autoscaler policy was last updated (UTC).",
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
					"scale_in_cooldown_period_minutes": {
						Description: "Cooldown period in minutes after a scale-in event.",
						Type:        types.Int64Type,
						Required:    true,
					},
					"scale_out_cooldown_period_minutes": {
						Description: "Cooldown period in minutes after a scale-out event.",
						Type:        types.Int64Type,
						Required:    true,
					},
					"post_maintenance_cooldown_period_minutes": {
						Description: "Cooldown period in minutes after maintenance completes.",
						Type:        types.Int64Type,
						Required:    true,
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
							"status": {
								Description: "Region policy status: ACTIVE or INACTIVE.",
								Type:        types.StringType,
								Computed:    true,
								Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("ACTIVE", "INACTIVE")},
							},
							"policies": {
								Description: "Scaling policies for the region.",
								Required:    true,
								Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
									"scalable_resource": {
										Description: "Resource type that can be scaled: NODE or CPU.",
										Type:        types.StringType,
										Required:    true,
										Validators:  []tfsdk.AttributeValidator{stringvalidator.OneOf("NODE", "CPU")},
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
											"name": {
												Description: "Name of the scaling rule.",
												Type:        types.StringType,
												Required:    true,
											},
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
											"scaling_action": {
												Description: "Scaling action to apply when this rule is satisfied.",
												Required:    true,
												Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
													"delta": {
														Description: "Number of nodes to scale in or out.",
														Type:        types.Int64Type,
														Required:    true,
													},
												}),
											},
											"metadata": {
												Description: "Metadata for the scaling rule.",
												Computed:    true,
												Attributes:  tfsdk.SingleNestedAttributes(autoscalerMetadataSchemaAttributes()),
											},
										}),
									},
									"metadata": {
										Description: "Metadata for the scaling policy.",
										Computed:    true,
										Attributes:  tfsdk.SingleNestedAttributes(autoscalerMetadataSchemaAttributes()),
									},
								}),
							},
							"metadata": {
								Description: "Metadata for the region policy.",
								Computed:    true,
								Attributes:  tfsdk.SingleNestedAttributes(autoscalerMetadataSchemaAttributes()),
							},
						}),
					},
					"metadata": {
						Description: "Metadata for the cluster-level policy.",
						Computed:    true,
						Attributes:  tfsdk.SingleNestedAttributes(autoscalerMetadataSchemaAttributes()),
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

// errAutoscalerPolicyNotFound indicates the autoscaler policy no longer exists upstream.
var errAutoscalerPolicyNotFound = errors.New("autoscaler policy not found")

func getAutoscalerPolicyState(ctx context.Context, state tfsdk.State, policy *AutoscalerPolicy) {
	state.GetAttribute(ctx, path.Root("account_id"), &policy.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &policy.ProjectID)
	state.GetAttribute(ctx, path.Root("cluster_id"), &policy.ClusterID)
	state.GetAttribute(ctx, path.Root("policy_id"), &policy.PolicyID)
	state.GetAttribute(ctx, path.Root("created_at"), &policy.CreatedAt)
	state.GetAttribute(ctx, path.Root("updated_at"), &policy.UpdatedAt)
	state.GetAttribute(ctx, path.Root("clusters"), &policy.Clusters)
}

func mapAutoscalerMetadata(metadata openapiclient.AutoscalerMetadata) *AutoscalerMetadata {
	return &AutoscalerMetadata{
		ID:        types.String{Value: metadata.GetId()},
		CreatedAt: types.String{Value: metadata.GetCreatedAt().UTC().Format(time.RFC3339)},
		UpdatedAt: types.String{Value: metadata.GetUpdatedAt().UTC().Format(time.RFC3339)},
	}
}

func mapOptionalAutoscalerMetadata(metadata *openapiclient.AutoscalerMetadata) *AutoscalerMetadata {
	if metadata == nil {
		return nil
	}
	return mapAutoscalerMetadata(*metadata)
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
		CreatedAt: types.String{Value: metadata.GetCreatedAt().UTC().Format(time.RFC3339)},
		UpdatedAt: types.String{Value: metadata.GetUpdatedAt().UTC().Format(time.RFC3339)},
		Clusters:  []AutoscalerPolicyCluster{},
	}

	for _, cluster := range data.GetClusters() {
		tfCluster := AutoscalerPolicyCluster{
			ClusterID:                            types.String{Value: cluster.GetClusterId()},
			Type:                                 types.String{Value: cluster.GetType()},
			ScaleInCooldownPeriodMinutes:         types.Int64{Value: int64(cluster.GetScaleInCooldownPeriodMinutes())},
			ScaleOutCooldownPeriodMinutes:        types.Int64{Value: int64(cluster.GetScaleOutCooldownPeriodMinutes())},
			PostMaintenanceCooldownPeriodMinutes: types.Int64{Value: int64(cluster.GetPostMaintenanceCooldownPeriodMinutes())},
			Regions:                              []AutoscalerPolicyClusterRegion{},
			Metadata:                             mapAutoscalerMetadata(cluster.GetMetadata()),
		}

		for _, region := range cluster.GetRegions() {
			tfRegion := AutoscalerPolicyClusterRegion{
				Code:     types.String{Value: region.GetCode()},
				Policies: []AutoscalerClusterRegionScalingPolicy{},
				Metadata: mapOptionalAutoscalerMetadata(region.Metadata),
			}
			if region.HasStatus() {
				tfRegion.Status = types.String{Value: string(region.GetStatus())}
			}

			for _, regionPolicy := range region.GetPolicies() {
				tfRules := make([]AutoscalerScalingRule, 0, len(regionPolicy.GetRules()))
				for _, rule := range regionPolicy.GetRules() {
					scalingAction := rule.GetScalingAction()
					tfRules = append(tfRules, AutoscalerScalingRule{
						Name:             types.String{Value: rule.GetName()},
						Resource:         types.String{Value: rule.GetResource()},
						Condition:        types.String{Value: rule.GetCondition()},
						Value:            types.Float64{Value: rule.GetValue()},
						EvaluationWindow: types.String{Value: rule.GetEvaluationWindow()},
						ScalingAction: &AutoscalerScalingAction{
							Delta: types.Int64{Value: int64(scalingAction.GetDelta())},
						},
						Metadata: mapOptionalAutoscalerMetadata(rule.Metadata),
					})
				}

				tfPolicy := AutoscalerClusterRegionScalingPolicy{
					ScalableResource: types.String{Value: string(regionPolicy.GetScalableResource())},
					Min:              types.Int64{Value: int64(regionPolicy.GetMin())},
					Max:              types.Int64{Value: int64(regionPolicy.GetMax())},
					ScalingType:      types.String{Value: string(regionPolicy.GetScalingType())},
					Clause:           types.String{Value: regionPolicy.GetClause()},
					Rules:            tfRules,
					Metadata:         mapOptionalAutoscalerMetadata(regionPolicy.Metadata),
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
	ctx context.Context,
	clusterId string,
	apiClient *openapiclient.APIClient,
) (AutoscalerPolicy, error) {
	accountId, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		return AutoscalerPolicy{}, errors.New(message)
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		return AutoscalerPolicy{}, errors.New(message)
	}

	resp, response, err := apiClient.AutoscalerApi.ListAutoscalerPolicies(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return AutoscalerPolicy{}, errAutoscalerPolicyNotFound
		}
		return AutoscalerPolicy{}, errors.New(getErrorMessage(response, err))
	}

	return mapAutoscalerPolicyFromResponse(accountId, projectId, clusterId, resp), nil
}

func (r resourceAutoscalerPolicy) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	resp.Diagnostics.AddError(
		"Autoscaler policy create not implemented",
		"Create for ybm_autoscaler_policy will be added in a follow-up change.",
	)
}

func (r resourceAutoscalerPolicy) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state AutoscalerPolicy
	getAutoscalerPolicyState(ctx, req.State, &state)

	policy, err := resourceAutoscalerPolicyRead(ctx, state.ClusterID.Value, r.p.client)
	if err != nil {
		if errors.Is(err, errAutoscalerPolicyNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read the state of the autoscaler policy", err.Error())
		return
	}

	diags := resp.State.Set(ctx, &policy)
	resp.Diagnostics.Append(diags...)
}

func (r resourceAutoscalerPolicy) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	resp.Diagnostics.AddError(
		"Autoscaler policy update not implemented",
		"Update for ybm_autoscaler_policy will be added in a follow-up change.",
	)
}

func (r resourceAutoscalerPolicy) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	resp.Diagnostics.AddError(
		"Autoscaler policy delete not implemented",
		"Delete for ybm_autoscaler_policy will be added in a follow-up change.",
	)
}

func (r resourceAutoscalerPolicy) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	// Import ID is the cluster ID used in the autoscaler policies API path.
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("cluster_id"), req, resp)
}
