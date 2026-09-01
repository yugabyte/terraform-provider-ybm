/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
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
		Description: `The resource to manage an autoscaler policy for a YugabyteDB Aeon cluster region.
Each resource corresponds to one policy for a specific cluster, region, and cluster type (PRIMARY or READ_REPLICA).
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
			"region": {
				Description: "Cloud region code this autoscaler policy applies to (for example, us-west1).",
				Type:        types.StringType,
				Required:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.RequiresReplace(),
				},
			},
			"type": {
				Description: "Cluster type: PRIMARY or READ_REPLICA.",
				Type:        types.StringType,
				Required:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.RequiresReplace(),
				},
			},
			"scalable_resource": {
				Description: "Resource type that can be scaled: NODE or CPU.",
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
			"status": {
				Description: "Policy status used to enable or disable autoscaling. Valid values are ACTIVE or INACTIVE.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					tfsdk.UseStateForUnknown(),
				},
			},
			"policy_rules": {
				Description: "Direction-specific scaling rules for the region policy.",
				Required:    true,
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
					"scaling_type": {
						Description: "Direction of scaling: SCALE_IN or SCALE_OUT.",
						Type:        types.StringType,
						Required:    true,
					},
					"clause": {
						Description: "Logical operator for combining scaling rules: AND or OR.",
						Type:        types.StringType,
						Required:    true,
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
							},
							"condition": {
								Description: "Comparison operator for the metric threshold: GT or LT.",
								Type:        types.StringType,
								Required:    true,
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
						Description: "Metadata for the direction-specific scaling policy.",
						Computed:    true,
						Attributes:  tfsdk.SingleNestedAttributes(autoscalerMetadataSchemaAttributes()),
					},
				}),
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

var errAutoscalerPolicyNotFound = errors.New("autoscaler policy not found")

type autoscalerPolicyPathParams struct {
	ClusterID   string
	Region      string
	ClusterType string
}

func getAutoscalerPolicyState(ctx context.Context, state tfsdk.State, policy *AutoscalerPolicy) {
	state.GetAttribute(ctx, path.Root("account_id"), &policy.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &policy.ProjectID)
	state.GetAttribute(ctx, path.Root("cluster_id"), &policy.ClusterID)
	state.GetAttribute(ctx, path.Root("region"), &policy.Region)
	state.GetAttribute(ctx, path.Root("type"), &policy.Type)
	state.GetAttribute(ctx, path.Root("scalable_resource"), &policy.ScalableResource)
	state.GetAttribute(ctx, path.Root("min"), &policy.Min)
	state.GetAttribute(ctx, path.Root("max"), &policy.Max)
	state.GetAttribute(ctx, path.Root("scale_in_cooldown_period_minutes"), &policy.ScaleInCooldownPeriodMinutes)
	state.GetAttribute(ctx, path.Root("scale_out_cooldown_period_minutes"), &policy.ScaleOutCooldownPeriodMinutes)
	state.GetAttribute(ctx, path.Root("post_maintenance_cooldown_period_minutes"), &policy.PostMaintenanceCooldownPeriodMinutes)
	state.GetAttribute(ctx, path.Root("status"), &policy.Status)
	state.GetAttribute(ctx, path.Root("policy_rules"), &policy.PolicyRules)
	state.GetAttribute(ctx, path.Root("policy_id"), &policy.PolicyID)
	state.GetAttribute(ctx, path.Root("created_at"), &policy.CreatedAt)
	state.GetAttribute(ctx, path.Root("updated_at"), &policy.UpdatedAt)
}

// Plan-only structs use types that can hold Unknown for computed nested fields.
// Decoding Unknown metadata into *AutoscalerMetadata fails with "unhandled unknown value".
type autoscalerPolicyPlanConfig struct {
	ClusterID                            types.String                 `tfsdk:"cluster_id"`
	Region                               types.String                 `tfsdk:"region"`
	Type                                 types.String                 `tfsdk:"type"`
	ScalableResource                     types.String                 `tfsdk:"scalable_resource"`
	Min                                  types.Int64                  `tfsdk:"min"`
	Max                                  types.Int64                  `tfsdk:"max"`
	ScaleInCooldownPeriodMinutes         types.Int64                  `tfsdk:"scale_in_cooldown_period_minutes"`
	ScaleOutCooldownPeriodMinutes        types.Int64                  `tfsdk:"scale_out_cooldown_period_minutes"`
	PostMaintenanceCooldownPeriodMinutes types.Int64                  `tfsdk:"post_maintenance_cooldown_period_minutes"`
	Status                               types.String                 `tfsdk:"status"`
	PolicyRules                          []autoscalerPolicyRuleConfig `tfsdk:"policy_rules"`
}

type autoscalerPolicyRuleConfig struct {
	ScalingType types.String                  `tfsdk:"scaling_type"`
	Clause      types.String                  `tfsdk:"clause"`
	Rules       []autoscalerScalingRuleConfig `tfsdk:"rules"`
	Metadata    types.Object                  `tfsdk:"metadata"`
}

type autoscalerScalingRuleConfig struct {
	Name             types.String                   `tfsdk:"name"`
	Resource         types.String                   `tfsdk:"resource"`
	Condition        types.String                   `tfsdk:"condition"`
	Value            types.Float64                  `tfsdk:"value"`
	EvaluationWindow types.String                   `tfsdk:"evaluation_window"`
	ScalingAction    *autoscalerScalingActionConfig `tfsdk:"scaling_action"`
	Metadata         types.Object                   `tfsdk:"metadata"`
}

type autoscalerScalingActionConfig struct {
	Delta types.Int64 `tfsdk:"delta"`
}

func getAutoscalerPolicyPlan(ctx context.Context, plan tfsdk.Plan, config *autoscalerPolicyPlanConfig) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &config.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("region"), &config.Region)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("type"), &config.Type)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("scalable_resource"), &config.ScalableResource)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("min"), &config.Min)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("max"), &config.Max)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("scale_in_cooldown_period_minutes"), &config.ScaleInCooldownPeriodMinutes)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("scale_out_cooldown_period_minutes"), &config.ScaleOutCooldownPeriodMinutes)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("post_maintenance_cooldown_period_minutes"), &config.PostMaintenanceCooldownPeriodMinutes)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("status"), &config.Status)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("policy_rules"), &config.PolicyRules)...)
	return diags
}

func autoscalerStatusSpecified(status types.String) bool {
	return !status.Unknown && !status.Null && status.Value != ""
}

func buildPolicyRulesSpec(rules []autoscalerPolicyRuleConfig) []openapiclient.AutoscalerClusterRegionPolicySpec {
	policyRules := make([]openapiclient.AutoscalerClusterRegionPolicySpec, 0, len(rules))
	for _, policyRule := range rules {
		scalingRules := make([]openapiclient.AutoscalerClusterRegionPolicyScalingRuleSpec, 0, len(policyRule.Rules))
		for _, rule := range policyRule.Rules {
			delta := int32(0)
			if rule.ScalingAction != nil {
				delta = int32(rule.ScalingAction.Delta.Value)
			}
			action := openapiclient.NewAutoscalerClusterRegionPolicyScalingActionSpec(delta)
			ruleSpec := openapiclient.NewAutoscalerClusterRegionPolicyScalingRuleSpec(
				rule.Name.Value,
				rule.Resource.Value,
				rule.Condition.Value,
				int32(rule.Value.Value),
				rule.EvaluationWindow.Value,
				*action,
			)
			scalingRules = append(scalingRules, *ruleSpec)
		}

		policyRules = append(policyRules, *openapiclient.NewAutoscalerClusterRegionPolicySpec(
			openapiclient.AutoscalerScalingDirectionEnum(policyRule.ScalingType.Value),
			policyRule.Clause.Value,
			scalingRules,
		))
	}
	return policyRules
}

func buildCreateAutoscalerPolicyRequestSpec(config autoscalerPolicyPlanConfig) openapiclient.CreateAutoscalerPolicyRequestSpec {
	spec := openapiclient.NewCreateAutoscalerPolicyRequestSpec(
		openapiclient.AutoscalerScalingDimensionEnum(config.ScalableResource.Value),
		int32(config.Min.Value),
		int32(config.Max.Value),
		int32(config.ScaleInCooldownPeriodMinutes.Value),
		int32(config.ScaleOutCooldownPeriodMinutes.Value),
		int32(config.PostMaintenanceCooldownPeriodMinutes.Value),
		buildPolicyRulesSpec(config.PolicyRules),
	)
	if autoscalerStatusSpecified(config.Status) {
		spec.SetStatus(openapiclient.AutoscalerPolicyStatusEnum(config.Status.Value))
	}
	return *spec
}

func buildUpdateAutoscalerPolicyRequestSpec(config autoscalerPolicyPlanConfig, fallbackStatus string) openapiclient.UpdateAutoscalerPolicyRequestSpec {
	status := fallbackStatus
	if autoscalerStatusSpecified(config.Status) {
		status = config.Status.Value
	}
	return *openapiclient.NewUpdateAutoscalerPolicyRequestSpec(
		openapiclient.AutoscalerScalingDimensionEnum(config.ScalableResource.Value),
		int32(config.Min.Value),
		int32(config.Max.Value),
		int32(config.ScaleInCooldownPeriodMinutes.Value),
		int32(config.ScaleOutCooldownPeriodMinutes.Value),
		int32(config.PostMaintenanceCooldownPeriodMinutes.Value),
		openapiclient.AutoscalerPolicyStatusEnum(status),
		buildPolicyRulesSpec(config.PolicyRules),
	)
}

func mapAutoscalerMetadata(metadata openapiclient.AutoscalerMetadata) *AutoscalerMetadata {
	return &AutoscalerMetadata{
		ID:        types.String{Value: metadata.GetId()},
		CreatedAt: types.String{Value: metadata.GetCreatedAt().UTC().Format(time.RFC3339)},
		UpdatedAt: types.String{Value: metadata.GetUpdatedAt().UTC().Format(time.RFC3339)},
	}
}

func mapAutoscalerPolicyFromResponse(
	accountID string,
	projectID string,
	params autoscalerPolicyPathParams,
	response openapiclient.AutoscalerPolicyResponse,
) (AutoscalerPolicy, error) {
	data := response.GetData()
	if data.GetRegion() != params.Region {
		return AutoscalerPolicy{}, errors.New("autoscaler policy region in API response does not match resource region")
	}
	if data.GetType() != params.ClusterType {
		return AutoscalerPolicy{}, errors.New("autoscaler policy type in API response does not match resource type")
	}
	if data.GetClusterId() != params.ClusterID {
		return AutoscalerPolicy{}, errors.New("autoscaler policy cluster_id in API response does not match resource cluster_id")
	}

	metadata := data.GetMetadata()

	policyRules := make([]AutoscalerPolicyRule, 0, len(data.GetPolicyRules()))
	for _, policyRule := range data.GetPolicyRules() {
		tfRules := make([]AutoscalerScalingRule, 0, len(policyRule.GetRules()))
		for _, rule := range policyRule.GetRules() {
			scalingAction := rule.GetScalingAction()
			ruleMetadata := rule.GetMetadata()
			tfRules = append(tfRules, AutoscalerScalingRule{
				Name:             types.String{Value: rule.GetName()},
				Resource:         types.String{Value: rule.GetResource()},
				Condition:        types.String{Value: rule.GetCondition()},
				Value:            types.Float64{Value: rule.GetValue()},
				EvaluationWindow: types.String{Value: rule.GetEvaluationWindow()},
				ScalingAction: &AutoscalerScalingAction{
					Delta: types.Int64{Value: int64(scalingAction.GetDelta())},
				},
				Metadata: mapAutoscalerMetadata(ruleMetadata),
			})
		}

		policyRuleMetadata := policyRule.GetMetadata()
		policyRules = append(policyRules, AutoscalerPolicyRule{
			ScalingType: types.String{Value: string(policyRule.GetScalingType())},
			Clause:      types.String{Value: policyRule.GetClause()},
			Rules:       tfRules,
			Metadata:    mapAutoscalerMetadata(policyRuleMetadata),
		})
	}

	return AutoscalerPolicy{
		AccountID:                            types.String{Value: accountID},
		ProjectID:                            types.String{Value: projectID},
		ClusterID:                            types.String{Value: data.GetClusterId()},
		Region:                               types.String{Value: data.GetRegion()},
		Type:                                 types.String{Value: data.GetType()},
		ScalableResource:                     types.String{Value: string(data.GetScalableResource())},
		Min:                                  types.Int64{Value: int64(data.GetMin())},
		Max:                                  types.Int64{Value: int64(data.GetMax())},
		ScaleInCooldownPeriodMinutes:         types.Int64{Value: int64(data.GetScaleInCooldownPeriodMinutes())},
		ScaleOutCooldownPeriodMinutes:        types.Int64{Value: int64(data.GetScaleOutCooldownPeriodMinutes())},
		PostMaintenanceCooldownPeriodMinutes: types.Int64{Value: int64(data.GetPostMaintenanceCooldownPeriodMinutes())},
		Status:                               types.String{Value: string(data.GetStatus())},
		PolicyRules:                          policyRules,
		PolicyID:                             types.String{Value: metadata.GetId()},
		CreatedAt:                            types.String{Value: metadata.GetCreatedAt().UTC().Format(time.RFC3339)},
		UpdatedAt:                            types.String{Value: metadata.GetUpdatedAt().UTC().Format(time.RFC3339)},
	}, nil
}

func resourceAutoscalerPolicyRead(
	ctx context.Context,
	params autoscalerPolicyPathParams,
	apiClient *openapiclient.APIClient,
) (AutoscalerPolicy, error) {
	accountID, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		return AutoscalerPolicy{}, errors.New(message)
	}

	projectID, getProjectOK, message := getProjectId(ctx, apiClient, accountID)
	if !getProjectOK {
		return AutoscalerPolicy{}, errors.New(message)
	}

	resp, response, err := apiClient.AutoscalerApi.GetAutoscalerPolicy(
		ctx,
		accountID,
		projectID,
		params.ClusterID,
		params.Region,
		params.ClusterType,
	).Execute()
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return AutoscalerPolicy{}, errAutoscalerPolicyNotFound
		}
		return AutoscalerPolicy{}, errors.New(getErrorMessage(response, err))
	}

	return mapAutoscalerPolicyFromResponse(accountID, projectID, params, resp)
}

func (r resourceAutoscalerPolicy) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var config autoscalerPolicyPlanConfig
	resp.Diagnostics.Append(getAutoscalerPolicyPlan(ctx, req.Plan, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient := r.p.client
	accountID, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectID, getProjectOK, message := getProjectId(ctx, apiClient, accountID)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID", message)
		return
	}

	params := autoscalerPolicyPathParams{
		ClusterID:   config.ClusterID.Value,
		Region:      config.Region.Value,
		ClusterType: config.Type.Value,
	}
	requestSpec := buildCreateAutoscalerPolicyRequestSpec(config)
	createResp, response, err := apiClient.AutoscalerApi.CreateAutoscalerPolicy(
		ctx,
		accountID,
		projectID,
		params.ClusterID,
		params.Region,
		params.ClusterType,
	).CreateAutoscalerPolicyRequestSpec(requestSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to create autoscaler policy", getErrorMessage(response, err))
		return
	}

	policy, err := mapAutoscalerPolicyFromResponse(accountID, projectID, params, createResp)
	if err != nil {
		resp.Diagnostics.AddError("Unable to map autoscaler policy response", err.Error())
		return
	}

	tflog.Debug(ctx, "Autoscaler policy created", map[string]interface{}{"policy": policy})

	diags := resp.State.Set(ctx, &policy)
	resp.Diagnostics.Append(diags...)
}

func (r resourceAutoscalerPolicy) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state AutoscalerPolicy
	getAutoscalerPolicyState(ctx, req.State, &state)

	params := autoscalerPolicyPathParams{
		ClusterID:   state.ClusterID.Value,
		Region:      state.Region.Value,
		ClusterType: state.Type.Value,
	}
	policy, err := resourceAutoscalerPolicyRead(ctx, params, r.p.client)
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
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var config autoscalerPolicyPlanConfig
	resp.Diagnostics.Append(getAutoscalerPolicyPlan(ctx, req.Plan, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state AutoscalerPolicy
	getAutoscalerPolicyState(ctx, req.State, &state)

	apiClient := r.p.client
	accountID, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectID, getProjectOK, message := getProjectId(ctx, apiClient, accountID)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID", message)
		return
	}

	params := autoscalerPolicyPathParams{
		ClusterID:   config.ClusterID.Value,
		Region:      config.Region.Value,
		ClusterType: config.Type.Value,
	}
	requestSpec := buildUpdateAutoscalerPolicyRequestSpec(config, state.Status.Value)
	updateResp, response, err := apiClient.AutoscalerApi.UpdateAutoscalerPolicy(
		ctx,
		accountID,
		projectID,
		params.ClusterID,
		params.Region,
		params.ClusterType,
	).UpdateAutoscalerPolicyRequestSpec(requestSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to update autoscaler policy", getErrorMessage(response, err))
		return
	}

	policy, err := mapAutoscalerPolicyFromResponse(accountID, projectID, params, updateResp)
	if err != nil {
		resp.Diagnostics.AddError("Unable to map autoscaler policy response", err.Error())
		return
	}

	tflog.Debug(ctx, "Autoscaler policy updated", map[string]interface{}{"policy": policy})

	diags := resp.State.Set(ctx, &policy)
	resp.Diagnostics.Append(diags...)
}

func (r resourceAutoscalerPolicy) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state AutoscalerPolicy
	getAutoscalerPolicyState(ctx, req.State, &state)

	apiClient := r.p.client
	accountID, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectID, getProjectOK, message := getProjectId(ctx, apiClient, accountID)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID", message)
		return
	}

	response, err := apiClient.AutoscalerApi.DeleteAutoscalerPolicy(
		ctx,
		accountID,
		projectID,
		state.ClusterID.Value,
		state.Region.Value,
		state.Type.Value,
	).Execute()
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to delete autoscaler policy", getErrorMessage(response, err))
		return
	}

	tflog.Debug(ctx, "Autoscaler policy deleted", map[string]interface{}{
		"cluster_id": state.ClusterID.Value,
		"region":     state.Region.Value,
		"type":       state.Type.Value,
	})
	resp.State.RemoveResource(ctx)
}

func (r resourceAutoscalerPolicy) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	idParts := strings.Split(req.ID, ",")
	if len(idParts) != 3 || idParts[0] == "" || idParts[1] == "" || idParts[2] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: cluster_id,region,type. Got: %q", req.ID),
		)
		return
	}

	resp.State.SetAttribute(ctx, path.Root("cluster_id"), idParts[0])
	resp.State.SetAttribute(ctx, path.Root("region"), idParts[1])
	resp.State.SetAttribute(ctx, path.Root("type"), idParts[2])
}
