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
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func autoscalerMetadataSchemaAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Description: "Entity ID.",
			Computed:    true,
		},
		"created_at": schema.StringAttribute{
			Description: "Timestamp when the entity was created (UTC).",
			Computed:    true,
		},
		"updated_at": schema.StringAttribute{
			Description: "Timestamp when the entity was last updated (UTC).",
			Computed:    true,
		},
	}
}

func (r *resourceAutoscalerPolicy) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The resource to manage an autoscaler policy for a YugabyteDB Aeon cluster region.
Each resource corresponds to one policy for a specific cluster, region, and cluster type (PRIMARY or READ_REPLICA).
Requires the AUTOSCALING feature flag (YBM_FF_AUTOSCALING=true).
When status is ACTIVE, set ignore_num_nodes_changes = true on the matching ybm_cluster cluster_region_info entry to prevent Terraform drift from autoscaler-driven node count changes.`,
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account this autoscaler policy belongs to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project this autoscaler policy belongs to.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"cluster_id": schema.StringAttribute{
				Description: "The ID of the cluster this autoscaler policy belongs to.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				Description: "Cloud region code this autoscaler policy applies to (for example, us-west1).",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"type": schema.StringAttribute{
				Description: "Cluster type: PRIMARY or READ_REPLICA.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"scalable_resource": schema.StringAttribute{
				Description: "Resource type that can be scaled: NODE or CPU.",
				Required:    true,
			},
			"min": schema.Int64Attribute{
				Description: "Minimum number of scalable resources.",
				Required:    true,
			},
			"max": schema.Int64Attribute{
				Description: "Maximum number of scalable resources.",
				Required:    true,
			},
			"scale_in_cooldown_period_minutes": schema.Int64Attribute{
				Description: "Cooldown period in minutes after a scale-in event.",
				Required:    true,
			},
			"scale_out_cooldown_period_minutes": schema.Int64Attribute{
				Description: "Cooldown period in minutes after a scale-out event.",
				Required:    true,
			},
			"post_maintenance_cooldown_period_minutes": schema.Int64Attribute{
				Description: "Cooldown period in minutes after maintenance completes.",
				Required:    true,
			},
			"status": schema.StringAttribute{
				Description: "Policy status used to enable or disable autoscaling. Valid values are ACTIVE or INACTIVE. When ACTIVE, set ignore_num_nodes_changes = true on the matching ybm_cluster region to prevent Terraform drift from autoscaler-driven num_nodes changes.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"policy_rules": schema.ListNestedAttribute{
				Description: "Direction-specific scaling rules for the region policy.",
				Required:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"scaling_type": schema.StringAttribute{
							Description: "Direction of scaling: SCALE_IN or SCALE_OUT.",
							Required:    true,
						},
						"clause": schema.StringAttribute{
							Description: "Logical operator for combining scaling rules: AND or OR.",
							Required:    true,
						},
						"rules": schema.ListNestedAttribute{
							Description: "Rules that must be satisfied to trigger scaling.",
							Required:    true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"name": schema.StringAttribute{
										Description: "Name of the scaling rule.",
										Required:    true,
									},
									"resource": schema.StringAttribute{
										Description: "Metric resource to evaluate: CPU or SQL_CONNECTION.",
										Required:    true,
									},
									"condition": schema.StringAttribute{
										Description: "Comparison operator for the metric threshold: GT or LT.",
										Required:    true,
									},
									"value": schema.Float64Attribute{
										Description: "Threshold value for the metric.",
										Required:    true,
									},
									"evaluation_window": schema.StringAttribute{
										Description: "Duration over which the metric is evaluated (for example, 5m).",
										Required:    true,
									},
									"scaling_action": schema.SingleNestedAttribute{
										Description: "Scaling action to apply when this rule is satisfied.",
										Required:    true,
										Attributes: map[string]schema.Attribute{
											"delta": schema.Int64Attribute{
												Description: "Number of nodes to scale in or out.",
												Required:    true,
											},
										},
									},
									"metadata": schema.SingleNestedAttribute{
										Description: "Metadata for the scaling rule.",
										Computed:    true,
										Attributes:  autoscalerMetadataSchemaAttributes(),
									},
								},
							},
						},
						"metadata": schema.SingleNestedAttribute{
							Description: "Metadata for the direction-specific scaling policy.",
							Computed:    true,
							Attributes:  autoscalerMetadataSchemaAttributes(),
						},
					},
				},
			},
			"policy_id": schema.StringAttribute{
				Description: "The ID of the autoscaler policy.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				Description: "Timestamp when the autoscaler policy was created (UTC).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Description: "Timestamp when the autoscaler policy was last updated (UTC).",
				Computed:    true,
			},
		},
	}
}

func newResourceAutoscalerPolicy() resource.Resource {
	return &resourceAutoscalerPolicy{}
}

func (r *resourceAutoscalerPolicy) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_autoscaler_policy"
}

func (r *resourceAutoscalerPolicy) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type resourceAutoscalerPolicy struct {
	p provider
}

var _ resource.ResourceWithModifyPlan = &resourceAutoscalerPolicy{}

var errAutoscalerPolicyNotFound = errors.New("autoscaler policy not found")

// ModifyPlan emits the ignore_num_nodes_changes drift warning during terraform plan
// (and again during apply) when the planned policy status is ACTIVE.
func (r resourceAutoscalerPolicy) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	resp.Plan = req.Plan

	// Destroy plans have a null planned state.
	if req.Plan.Raw.IsNull() {
		return
	}

	var status types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("status"), &status)...)
	if resp.Diagnostics.HasError() || !autoscalerStatusSpecified(status) {
		return
	}

	appendAutoscalerDriftWarning(&resp.Diagnostics, status.ValueString())
}

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
	return !status.IsUnknown() && !status.IsNull() && status.ValueString() != ""
}

func buildPolicyRulesSpec(rules []autoscalerPolicyRuleConfig) []openapiclient.AutoscalerClusterRegionPolicySpec {
	policyRules := make([]openapiclient.AutoscalerClusterRegionPolicySpec, 0, len(rules))
	for _, policyRule := range rules {
		scalingRules := make([]openapiclient.AutoscalerClusterRegionPolicyScalingRuleSpec, 0, len(policyRule.Rules))
		for _, rule := range policyRule.Rules {
			delta := int32(0)
			if rule.ScalingAction != nil {
				delta = int32(rule.ScalingAction.Delta.ValueInt64())
			}
			action := openapiclient.NewAutoscalerClusterRegionPolicyScalingActionSpec(delta)
			ruleSpec := openapiclient.NewAutoscalerClusterRegionPolicyScalingRuleSpec(
				rule.Name.ValueString(),
				rule.Resource.ValueString(),
				rule.Condition.ValueString(),
				int32(rule.Value.ValueFloat64()),
				rule.EvaluationWindow.ValueString(),
				*action,
			)
			scalingRules = append(scalingRules, *ruleSpec)
		}

		policyRules = append(policyRules, *openapiclient.NewAutoscalerClusterRegionPolicySpec(
			openapiclient.AutoscalerScalingDirectionEnum(policyRule.ScalingType.ValueString()),
			policyRule.Clause.ValueString(),
			scalingRules,
		))
	}
	return policyRules
}

func buildCreateAutoscalerPolicyRequestSpec(config autoscalerPolicyPlanConfig) openapiclient.CreateAutoscalerPolicyRequestSpec {
	spec := openapiclient.NewCreateAutoscalerPolicyRequestSpec(
		openapiclient.AutoscalerScalingDimensionEnum(config.ScalableResource.ValueString()),
		int32(config.Min.ValueInt64()),
		int32(config.Max.ValueInt64()),
		int32(config.ScaleInCooldownPeriodMinutes.ValueInt64()),
		int32(config.ScaleOutCooldownPeriodMinutes.ValueInt64()),
		int32(config.PostMaintenanceCooldownPeriodMinutes.ValueInt64()),
		buildPolicyRulesSpec(config.PolicyRules),
	)
	if autoscalerStatusSpecified(config.Status) {
		spec.SetStatus(openapiclient.AutoscalerPolicyStatusEnum(config.Status.ValueString()))
	}
	return *spec
}

func buildUpdateAutoscalerPolicyRequestSpec(config autoscalerPolicyPlanConfig, fallbackStatus string) openapiclient.UpdateAutoscalerPolicyRequestSpec {
	status := fallbackStatus
	if autoscalerStatusSpecified(config.Status) {
		status = config.Status.ValueString()
	}
	return *openapiclient.NewUpdateAutoscalerPolicyRequestSpec(
		openapiclient.AutoscalerScalingDimensionEnum(config.ScalableResource.ValueString()),
		int32(config.Min.ValueInt64()),
		int32(config.Max.ValueInt64()),
		int32(config.ScaleInCooldownPeriodMinutes.ValueInt64()),
		int32(config.ScaleOutCooldownPeriodMinutes.ValueInt64()),
		int32(config.PostMaintenanceCooldownPeriodMinutes.ValueInt64()),
		openapiclient.AutoscalerPolicyStatusEnum(status),
		buildPolicyRulesSpec(config.PolicyRules),
	)
}

func mapAutoscalerMetadata(metadata openapiclient.AutoscalerMetadata) *AutoscalerMetadata {
	return &AutoscalerMetadata{
		ID:        types.StringValue(metadata.GetId()),
		CreatedAt: types.StringValue(metadata.GetCreatedAt().UTC().Format(time.RFC3339)),
		UpdatedAt: types.StringValue(metadata.GetUpdatedAt().UTC().Format(time.RFC3339)),
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
				Name:             types.StringValue(rule.GetName()),
				Resource:         types.StringValue(rule.GetResource()),
				Condition:        types.StringValue(rule.GetCondition()),
				Value:            types.Float64Value(rule.GetValue()),
				EvaluationWindow: types.StringValue(rule.GetEvaluationWindow()),
				ScalingAction: &AutoscalerScalingAction{
					Delta: types.Int64Value(int64(scalingAction.GetDelta())),
				},
				Metadata: mapAutoscalerMetadata(ruleMetadata),
			})
		}

		policyRuleMetadata := policyRule.GetMetadata()
		policyRules = append(policyRules, AutoscalerPolicyRule{
			ScalingType: types.StringValue(string(policyRule.GetScalingType())),
			Clause:      types.StringValue(policyRule.GetClause()),
			Rules:       tfRules,
			Metadata:    mapAutoscalerMetadata(policyRuleMetadata),
		})
	}

	return AutoscalerPolicy{
		AccountID:                            types.StringValue(accountID),
		ProjectID:                            types.StringValue(projectID),
		ClusterID:                            types.StringValue(data.GetClusterId()),
		Region:                               types.StringValue(data.GetRegion()),
		Type:                                 types.StringValue(data.GetType()),
		ScalableResource:                     types.StringValue(string(data.GetScalableResource())),
		Min:                                  types.Int64Value(int64(data.GetMin())),
		Max:                                  types.Int64Value(int64(data.GetMax())),
		ScaleInCooldownPeriodMinutes:         types.Int64Value(int64(data.GetScaleInCooldownPeriodMinutes())),
		ScaleOutCooldownPeriodMinutes:        types.Int64Value(int64(data.GetScaleOutCooldownPeriodMinutes())),
		PostMaintenanceCooldownPeriodMinutes: types.Int64Value(int64(data.GetPostMaintenanceCooldownPeriodMinutes())),
		Status:                               types.StringValue(string(data.GetStatus())),
		PolicyRules:                          policyRules,
		PolicyID:                             types.StringValue(metadata.GetId()),
		CreatedAt:                            types.StringValue(metadata.GetCreatedAt().UTC().Format(time.RFC3339)),
		UpdatedAt:                            types.StringValue(metadata.GetUpdatedAt().UTC().Format(time.RFC3339)),
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

func (r resourceAutoscalerPolicy) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
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
		ClusterID:   config.ClusterID.ValueString(),
		Region:      config.Region.ValueString(),
		ClusterType: config.Type.ValueString(),
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

func (r resourceAutoscalerPolicy) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AutoscalerPolicy
	getAutoscalerPolicyState(ctx, req.State, &state)

	params := autoscalerPolicyPathParams{
		ClusterID:   state.ClusterID.ValueString(),
		Region:      state.Region.ValueString(),
		ClusterType: state.Type.ValueString(),
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

func (r resourceAutoscalerPolicy) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
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
		ClusterID:   config.ClusterID.ValueString(),
		Region:      config.Region.ValueString(),
		ClusterType: config.Type.ValueString(),
	}
	requestSpec := buildUpdateAutoscalerPolicyRequestSpec(config, state.Status.ValueString())
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

func (r resourceAutoscalerPolicy) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
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
		state.ClusterID.ValueString(),
		state.Region.ValueString(),
		state.Type.ValueString(),
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
		"cluster_id": state.ClusterID.ValueString(),
		"region":     state.Region.ValueString(),
		"type":       state.Type.ValueString(),
	})
	resp.State.RemoveResource(ctx)
}

func (r resourceAutoscalerPolicy) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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
