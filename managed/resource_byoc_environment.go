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
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

const byocEnvironmentDeregistered = "DEREGISTERED"

type resourceByocEnvironmentType struct{}

func (r resourceByocEnvironmentType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `The resource to register a BYOC (bring your own cloud) environment for PostgreSQL clusters. ` +
			`Registering returns an API key for the environment's agent; install the agent in your Kubernetes cluster ` +
			`with install_hint, and the environment becomes READY once the agent enrolls. ` +
			`The key is only returned by the apply that registers the environment: importing cannot recover it, and replacing ` +
			`or destroying the environment revokes it. An environment's name stays claimed after it is destroyed, so it cannot be registered again.`,
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description:   "The ID of the account this environment belongs to.",
				Type:          types.StringType,
				Computed:      true,
				PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
			},
			"environment_id": {
				Description:   "The ID of the environment, derived from its name.",
				Type:          types.StringType,
				Computed:      true,
				PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
			},
			"name": {
				Description: "The name of the environment, 3 to 60 characters. Its environment_id is derived from it: lowercased, " +
					"with spaces, underscores and slashes turned into hyphens. A change that derives the same ID, such as the case, " +
					"is applied in place, so after an import the name may be written as registered or as the ID. " +
					"Any other change forces a new environment.",
				Type:       types.StringType,
				Required:   true,
				Validators: []tfsdk.AttributeValidator{stringvalidator.LengthBetween(3, 60)},
			},
			"region": {
				Description: "The region the environment's agent reported when it enrolled.",
				Type:        types.StringType,
				Computed:    true,
			},
			"placement": {
				Description: "Where the environment runs, which the install command passes to the agent. Defaults to cloud. " +
					"It only applies when the environment is registered.",
				Type:          types.StringType,
				Optional:      true,
				Computed:      true,
				PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
			},
			"state": {
				Description: "The state of the environment, such as PENDING, ENROLLING or READY.",
				Type:        types.StringType,
				Computed:    true,
			},
			"cloud_provider": {
				Description: "The cloud provider the agent reported.",
				Type:        types.StringType,
				Computed:    true,
			},
			"k8s_version": {
				Description: "The Kubernetes version the agent reported.",
				Type:        types.StringType,
				Computed:    true,
			},
			"agent_api_key": {
				Description:   "The API key the environment's agent authenticates with. Only available from the apply that registers the environment, and not after an import.",
				Type:          types.StringType,
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
			},
			"install_hint": {
				Description: "The command that installs the environment's agent.",
				Type:        types.StringType,
				Computed:    true,
			},
		},
	}, nil
}

func (r resourceByocEnvironmentType) NewResource(_ context.Context, p tfsdk.Provider) (tfsdk.Resource, diag.Diagnostics) {
	return resourceByocEnvironment{
		p: *(p.(*provider)),
	}, nil
}

type resourceByocEnvironment struct {
	p provider
}

// byocEnvironmentID derives an environment's ID from its name the way YCP does.
func byocEnvironmentID(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	return strings.NewReplacer(" ", "-", "_", "-", "/", "-").Replace(s)
}

// ModifyPlan replaces the environment only for a name that derives another ID, since a
// replacement revokes the agent's key and the old name stays claimed.
func (r resourceByocEnvironment) ModifyPlan(ctx context.Context, req tfsdk.ModifyResourcePlanRequest, resp *tfsdk.ModifyResourcePlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var name, environmentId types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("name"), &name)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("environment_id"), &environmentId)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if name.Unknown || byocEnvironmentID(name.Value) != environmentId.Value {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("name"))
	}
}

type ycpEnvironment struct {
	ID          string        `json:"id"`
	AccountID   string        `json:"account_id"`
	Placement   string        `json:"placement"`
	Region      string        `json:"region"`
	State       string        `json:"state"`
	Provider    string        `json:"provider"`
	K8sVersion  string        `json:"k8s_version"`
	InstallHint string        `json:"install_hint"`
	Resources   []ampObserved `json:"resources"`
}

func byocEnvironmentToState(accountId string, name string, env ycpEnvironment, apiKey types.String, installHint string) ByocEnvironment {
	if installHint == "" {
		installHint = env.InstallHint
	}
	return ByocEnvironment{
		AccountID:     types.String{Value: accountId},
		EnvironmentID: types.String{Value: env.ID},
		Name:          types.String{Value: name},
		Region:        tfString(env.Region),
		Placement:     tfString(env.Placement),
		State:         tfString(env.State),
		Provider:      tfString(env.Provider),
		K8sVersion:    tfString(env.K8sVersion),
		AgentApiKey:   apiKey,
		InstallHint:   tfString(installHint),
	}
}

func getByocEnvironment(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, environmentId string) (ycpEnvironment, error) {
	var env ycpEnvironment
	data, err := ycpDo(ctx, apiClient, http.MethodGet, accountId, "/environments/"+url.PathEscape(environmentId), nil, nil)
	if err != nil {
		return env, err
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return env, fmt.Errorf("unable to parse environment %s: %v", environmentId, err)
	}
	return env, nil
}

func (r resourceByocEnvironment) Create(ctx context.Context, req tfsdk.CreateResourceRequest, resp *tfsdk.CreateResourceResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan ByocEnvironment
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	apiClient := r.p.client

	accountId, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	body := map[string]interface{}{
		"name": plan.Name.Value,
		"kind": "amp",
		"type": "BYOC",
	}
	if known(plan.Placement.Null, plan.Placement.Unknown) {
		body["placement"] = plan.Placement.Value
	}
	data, err := ycpDo(ctx, apiClient, http.MethodPost, accountId, "/environments", nil, body)
	if err != nil {
		resp.Diagnostics.AddError("Unable to register environment "+plan.Name.Value, err.Error())
		return
	}
	var registered struct {
		Environment ycpEnvironment `json:"environment"`
		InstallHint string         `json:"install_hint"`
		ApiKey      string         `json:"api_key"`
	}
	if err := json.Unmarshal(data, &registered); err != nil {
		resp.Diagnostics.AddError("Unable to parse the registered environment", err.Error())
		return
	}

	state := byocEnvironmentToState(accountId, plan.Name.Value, registered.Environment, tfString(registered.ApiKey), registered.InstallHint)
	if known(plan.Placement.Null, plan.Placement.Unknown) {
		state.Placement = plan.Placement
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r resourceByocEnvironment) Read(ctx context.Context, req tfsdk.ReadResourceRequest, resp *tfsdk.ReadResourceResponse) {
	var state ByocEnvironment
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

	env, err := getByocEnvironment(ctx, r.p.client, accountId, state.EnvironmentID.Value)
	if isYcpNotFound(err) || (err == nil && env.State == byocEnvironmentDeregistered) {
		tflog.Warn(ctx, "Environment "+state.EnvironmentID.Value+" no longer exists; removing it from state")
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read environment "+state.EnvironmentID.Value, err.Error())
		return
	}

	name := state.Name.Value
	if name == "" {
		// An import knows only the ID, which is derived from the name.
		name = env.ID
	}
	newState := byocEnvironmentToState(accountId, name, env, state.AgentApiKey, "")
	if newState.AgentApiKey.Unknown {
		newState.AgentApiKey = types.String{Null: true}
	}
	// The agent reports its own placement when it enrolls; the configured one is what was registered.
	if known(state.Placement.Null, state.Placement.Unknown) {
		newState.Placement = state.Placement
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

// Update records a name that derives the same ID and a placement, which only applies at
// registration, and refreshes the computed attributes.
func (r resourceByocEnvironment) Update(ctx context.Context, req tfsdk.UpdateResourceRequest, resp *tfsdk.UpdateResourceResponse) {
	var plan, state ByocEnvironment
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	env, err := getByocEnvironment(ctx, r.p.client, state.AccountID.Value, state.EnvironmentID.Value)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read environment "+state.EnvironmentID.Value, err.Error())
		return
	}
	newState := byocEnvironmentToState(state.AccountID.Value, plan.Name.Value, env, state.AgentApiKey, "")
	if known(plan.Placement.Null, plan.Placement.Unknown) {
		newState.Placement = plan.Placement
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r resourceByocEnvironment) Delete(ctx context.Context, req tfsdk.DeleteResourceRequest, resp *tfsdk.DeleteResourceResponse) {
	var state ByocEnvironment
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Deregistering also revokes the agent's API key.
	_, err := ycpDo(ctx, r.p.client, http.MethodDelete, state.AccountID.Value, "/environments/"+url.PathEscape(state.EnvironmentID.Value), nil, nil)
	if err != nil && !isYcpNotFound(err) {
		resp.Diagnostics.AddError("Unable to deregister environment "+state.EnvironmentID.Value, err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}

// ImportState imports an environment by its ID.
func (r resourceByocEnvironment) ImportState(ctx context.Context, req tfsdk.ImportResourceStateRequest, resp *tfsdk.ImportResourceStateResponse) {
	tfsdk.ResourceImportStatePassthroughID(ctx, path.Root("environment_id"), req, resp)
}
