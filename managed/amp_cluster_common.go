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
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	ampRoleReadWrite = "read_write"
	ampRoleReadOnly  = "read_only"
)

// Vars so tests can poll without waiting.
var (
	ampPollInterval     = 10 * time.Second
	ampOperationTimeout = 60 * time.Minute
)

// Names become a segment of the resource path, so they cannot contain a slash.
var ampNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

const ampNameRule = "must be lowercase letters, digits and hyphens, starting with a letter or digit"

// Kubernetes quantities: CPU in cores or millicores, memory in bytes with an optional suffix.
var (
	ampCpuPattern    = regexp.MustCompile(`^([0-9]+(\.[0-9]+)?|[0-9]+m)$`)
	ampMemoryPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?(k|M|G|T|P|E|Ki|Mi|Gi|Ti|Pi|Ei)?$`)
)

var ampEngines = []string{"postgres", "aidb", "oracle", "sqlserver", "documentdb"}
var ampConnectivityModes = []string{"cluster_ip", "ingress_internal", "ingress_external", "nlb_internal", "nlb_external"}

const ampSettingsManaged = "Leave it unset to leave the settings as they are, such as those a fork inherits from its parent; " +
	"set it to {} to clear them. Removing a key resets the setting: an endpoint setting falls back to its branch's value, " +
	"and a branch setting to the PostgreSQL default."

func ampNameAttribute(description string) tfsdk.Attribute {
	return tfsdk.Attribute{
		Description:   description,
		Type:          types.StringType,
		Required:      true,
		Validators:    []tfsdk.AttributeValidator{stringvalidator.RegexMatches(ampNamePattern, ampNameRule)},
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.RequiresReplace()},
	}
}

func ampPgSettingsAttribute(description string) tfsdk.Attribute {
	return tfsdk.Attribute{
		Description: description + " Can be changed in place. " + ampSettingsManaged,
		Type:        types.MapType{ElemType: types.StringType},
		Optional:    true,
	}
}

func ampExtensionsAttribute(description string) tfsdk.Attribute {
	return tfsdk.Attribute{
		Description: description + " Extensions can be added in place but not removed.",
		Type:        types.SetType{ElemType: types.StringType},
		Optional:    true,
	}
}

func ampForceDestroyAttribute(description string) tfsdk.Attribute {
	return tfsdk.Attribute{
		Description: description + " Defaults to false. It is read from state, so apply it before the destroy or replacement that needs it.",
		Type:        types.BoolType,
		Optional:    true,
	}
}

func ampCredentialsAttribute(description string) tfsdk.Attribute {
	return tfsdk.Attribute{
		Description: description,
		Optional:    true,
		Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
			"username": {
				Description: "The user. Only the default, " + ampUsername + ", is supported for now.",
				Type:        types.StringType,
				Optional:    true,
			},
			"password": {
				Description: "The password, 8 to 128 printable ASCII characters. It is stored in state as configured and never read back. " +
					"It cannot be changed yet, since passwords cannot be rotated.",
				Type:       types.StringType,
				Optional:   true,
				Sensitive:  true,
				Validators: []tfsdk.AttributeValidator{stringvalidator.LengthBetween(8, 128)},
			},
			"password_wo": {
				Description: "Reserved for a write-only password that is never stored in state. Not supported yet.",
				Type:        types.StringType,
				Optional:    true,
				Sensitive:   true,
			},
			"password_wo_version": {
				Description: "Reserved for rotating password_wo: changing it sets the new password. Not supported yet.",
				Type:        types.Int64Type,
				Optional:    true,
			},
		}),
	}
}

// ampEndpointAttributes are the attributes of the ybm_aeon_pg_endpoint resource's compute and
// connection details.
func ampEndpointAttributes() map[string]tfsdk.Attribute {
	return map[string]tfsdk.Attribute{
		"cpu": {
			Description:   "The CPU of the endpoint as a Kubernetes quantity, such as 2 or 500m. Defaults to the service's default. Can be changed in place to resize the endpoint.",
			Type:          types.StringType,
			Optional:      true,
			Computed:      true,
			Validators:    []tfsdk.AttributeValidator{stringvalidator.RegexMatches(ampCpuPattern, "must be a CPU quantity such as 2, 0.5 or 500m")},
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
		},
		"memory": {
			Description: "The memory of the endpoint as a Kubernetes quantity, such as 16Gi. Must be at least 8Gi per vCPU. " +
				"Defaults to the service's default for the cpu. When it is unset, changing cpu resizes memory to 8Gi per vCPU. Can be changed in place.",
			Type:       types.StringType,
			Optional:   true,
			Computed:   true,
			Validators: []tfsdk.AttributeValidator{stringvalidator.RegexMatches(ampMemoryPattern, "must be a memory quantity such as 16Gi or 512Mi")},
		},
		"suspend_timeout_seconds": {
			Description: "Reserved for suspending the endpoint after this many idle seconds. Not supported yet.",
			Type:        types.Int64Type,
			Optional:    true,
		},
		"connectivity_mode": {
			Description:   "How the endpoint is exposed: " + strings.Join(ampConnectivityModes, ", ") + ". Defaults to the service's default. Changing it forces a replacement.",
			Type:          types.StringType,
			Optional:      true,
			Computed:      true,
			Validators:    []tfsdk.AttributeValidator{stringvalidator.OneOf(ampConnectivityModes...)},
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown(), tfsdk.RequiresReplace()},
		},
		"allowed_cidrs": {
			Description: "CIDR blocks allowed to connect to the endpoint. Leave it unset to leave them as they are.",
			Type:        types.SetType{ElemType: types.StringType},
			Optional:    true,
		},
		"state": {
			Description: "The state the endpoint last reported, such as ready or provisioning.",
			Type:        types.StringType,
			Computed:    true,
		},
		"host": {
			Description:   "The host of the endpoint.",
			Type:          types.StringType,
			Computed:      true,
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
		},
		"port": {
			Description:   "The port of the endpoint.",
			Type:          types.StringType,
			Computed:      true,
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
		},
		"username": {
			Description:   "The user to connect as.",
			Type:          types.StringType,
			Computed:      true,
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
		},
		"database": {
			Description:   "The database to connect to.",
			Type:          types.StringType,
			Computed:      true,
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
		},
		"tls_ca_certificate": {
			Description:   "The CA certificate to verify the endpoint's TLS certificate with, when the endpoint requires one.",
			Type:          types.StringType,
			Computed:      true,
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
		},
	}
}

// ampClusterAttributes are the attributes shared by the shared and BYOC cluster resources: a
// cluster and its main branch. Endpoints, including the read-write one, are ybm_aeon_pg_endpoint
// resources.
func ampClusterAttributes() map[string]tfsdk.Attribute {
	return map[string]tfsdk.Attribute{
		"account_id": {
			Description:   "The ID of the account this cluster belongs to.",
			Type:          types.StringType,
			Computed:      true,
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
		},
		"cluster_name": ampNameAttribute("The name of the cluster. Must be unique within its storage pool. Changing it forces a new cluster."),
		"pg_version": {
			Description:   "The PostgreSQL major version. Defaults to the service's default. Changing it forces a new cluster.",
			Type:          types.Int64Type,
			Optional:      true,
			Computed:      true,
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown(), tfsdk.RequiresReplace()},
		},
		"engine": {
			Description:   "The database engine: " + strings.Join(ampEngines, ", ") + ". Defaults to postgres. Changing it forces a new cluster.",
			Type:          types.StringType,
			Optional:      true,
			Computed:      true,
			Validators:    []tfsdk.AttributeValidator{stringvalidator.OneOf(ampEngines...)},
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown(), tfsdk.RequiresReplace()},
		},
		"extensions": {
			Description:   "Extensions to install in the cluster. Changing it forces a new cluster; use branch_extensions to add extensions to the main branch in place.",
			Type:          types.SetType{ElemType: types.StringType},
			Optional:      true,
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.RequiresReplace()},
		},
		"cluster_id": {
			Description:   "The ID of the cluster. Set a branch's or endpoint's cluster_id to it so that replacing the cluster replaces them too.",
			Type:          types.StringType,
			Computed:      true,
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
		},
		"state": {
			Description: "The state the cluster last reported, such as ready or provisioning.",
			Type:        types.StringType,
			Computed:    true,
		},
		"branch_pg_settings": ampPgSettingsAttribute(
			"PostgreSQL settings of the main branch, such as work_mem. They apply to every endpoint on main; an endpoint's own pg_settings override them."),
		"branch_extensions": ampExtensionsAttribute("Extensions to install in the main branch."),
		"force_destroy": ampForceDestroyAttribute(
			"Whether destroying or replacing the cluster may also delete its endpoints and the branches other than main. " +
				"Without it, deleting a cluster that still has them fails."),
	}
}

// ampEndpointConfig is the configuration of an endpoint. Empty strings and nil collections are
// not sent, so the service chooses or keeps them.
type ampEndpointConfig struct {
	Role             string
	Password         string
	Cpu              string
	Memory           string
	ConnectivityMode string
	// nil when Terraform does not manage them.
	AllowedCidrs *[]string
	PgSettings   *map[string]string
}

// ampClusterConfig is the configuration of a cluster and its main branch.
type ampClusterConfig struct {
	PgVersion        int64
	Engine           string
	Extensions       []string
	BranchPgSettings *map[string]string
	BranchExtensions []string
}

func ampPassword(c *AmpCredentials) string {
	if c == nil {
		return ""
	}
	return c.Password.Value
}

func newAmpEndpointConfig(role string, plan AmpEndpoint, prior *AmpEndpoint) (ampEndpointConfig, error) {
	c := ampEndpointConfig{
		Role:             role,
		Password:         ampPassword(plan.Credentials),
		Cpu:              stringOr(plan.Cpu, ""),
		Memory:           stringOr(plan.Memory, ""),
		ConnectivityMode: stringOr(plan.ConnectivityMode, ""),
	}
	var priorCidrs []types.String
	priorPg := types.Map{Null: true}
	if prior != nil {
		priorCidrs, priorPg = prior.AllowedCidrs, prior.PgSettings
	}
	c.AllowedCidrs = managedStrings(plan.AllowedCidrs, priorCidrs)
	c.PgSettings = managedMap(plan.PgSettings, priorPg)
	// AMP wants cpu and memory declared together on a resize, so an unset memory follows the new
	// cpu the way the service defaults it.
	if prior != nil && c.Memory == "" && c.Cpu != "" {
		m, err := ampDefaultMemory(c.Cpu)
		if err != nil {
			return c, err
		}
		c.Memory = m
	}
	return c, nil
}

func newAmpClusterConfig(pgVersion types.Int64, engine types.String, extensions []types.String,
	branchPgSettings types.Map, branchExtensions []types.String, priorBranchPgSettings types.Map) ampClusterConfig {
	c := ampClusterConfig{
		Engine:           stringOr(engine, ""),
		Extensions:       stringsFromTf(extensions),
		BranchPgSettings: managedMap(branchPgSettings, priorBranchPgSettings),
		BranchExtensions: stringsFromTf(branchExtensions),
	}
	if known(pgVersion.Null, pgVersion.Unknown) {
		c.PgVersion = pgVersion.Value
	}
	return c
}

func known(null, unknown bool) bool {
	return !null && !unknown
}

func stringsFromTf(values []types.String) []string {
	if values == nil {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, v.Value)
	}
	return out
}

// stringsToTf returns the collection to store: nil when Terraform does not manage it, as when
// it was never configured, and otherwise the service's values, empty rather than nil so the
// read-back matches the plan.
func stringsToTf(values []string, prior []types.String) []types.String {
	if prior == nil {
		return nil
	}
	out := make([]types.String, 0, len(values))
	for _, v := range values {
		out = append(out, types.String{Value: v})
	}
	return out
}

// managedStrings returns what to send for a collection: the configured values, an empty one to
// clear the values when the configuration was removed, or nil to leave them alone.
func managedStrings(values []types.String, prior []types.String) *[]string {
	switch {
	case values != nil:
		out := stringsFromTf(values)
		return &out
	case prior != nil:
		return &[]string{}
	}
	return nil
}

func mapFromTf(m types.Map) map[string]string {
	out := map[string]string{}
	for k, v := range m.Elems {
		if s, ok := v.(types.String); ok {
			out[k] = s.Value
		}
	}
	return out
}

// managedMap is managedStrings for a map.
func managedMap(m types.Map, prior types.Map) *map[string]string {
	switch {
	case known(m.Null, m.Unknown):
		out := mapFromTf(m)
		return &out
	case known(prior.Null, prior.Unknown):
		return &map[string]string{}
	}
	return nil
}

// mapToTf is stringsToTf for a map.
func mapToTf(m map[string]string, prior types.Map) types.Map {
	if !known(prior.Null, prior.Unknown) {
		return types.Map{ElemType: types.StringType, Null: true}
	}
	elems := make(map[string]attr.Value, len(m))
	for k, v := range m {
		elems[k] = types.String{Value: v}
	}
	return types.Map{ElemType: types.StringType, Elems: elems}
}

// ampCredentialsToTf returns the credentials to store. The password is never read back: the
// service may stop returning it, or return a verifier instead, so the configured one is kept.
func ampCredentialsToTf(prior *AmpCredentials) *AmpCredentials {
	if prior == nil {
		return nil
	}
	c := *prior
	if c.Password.Unknown {
		c.Password = types.String{Null: true}
	}
	return &c
}

// ampDefaultMemory returns 8Gi per vCPU of the given Kubernetes CPU quantity.
func ampDefaultMemory(cpu string) (string, error) {
	if strings.HasSuffix(cpu, "m") {
		milli, err := strconv.ParseInt(strings.TrimSuffix(cpu, "m"), 10, 64)
		if err != nil || milli <= 0 {
			return "", fmt.Errorf("cpu %q is not a valid quantity; set memory explicitly", cpu)
		}
		return fmt.Sprintf("%dMi", milli*8192/1000), nil
	}
	vcpus, err := strconv.ParseFloat(cpu, 64)
	if err != nil || vcpus <= 0 {
		return "", fmt.Errorf("cpu %q is not a valid quantity; set memory explicitly", cpu)
	}
	return fmt.Sprintf("%dMi", int64(vcpus*8192)), nil
}

func (c ampClusterConfig) clusterSpec() ampClusterSpec {
	return ampClusterSpec{PgVersion: c.PgVersion, Engine: c.Engine, Extensions: c.Extensions}
}

// mainBranchSpec returns the main branch's spec. A new branch takes no extensions, so they are
// only sent once the branch exists.
func (c ampClusterConfig) mainBranchSpec(exists bool) ampBranchSpec {
	spec := ampBranchSpec{PgSettings: c.BranchPgSettings}
	if exists {
		spec.Extensions = c.BranchExtensions
	}
	return spec
}

// spec returns the endpoint's spec. A full spec carries the create-time fields; otherwise only the
// fields AMP can change in place are sent. Nested maps are sent whole, since an update replaces
// them rather than merging them.
func (c ampEndpointConfig) spec(full bool) ampEndpointSpec {
	spec := ampEndpointSpec{PgSettings: c.PgSettings}
	if c.Cpu != "" || c.Memory != "" {
		spec.Resources = &ampEndpointResources{CPU: c.Cpu, Memory: c.Memory}
	}
	// An update replaces connectivity whole, so it is sent only to change allowed_cidrs, and then
	// with the mode it already has.
	if c.AllowedCidrs != nil || (full && c.ConnectivityMode != "") {
		spec.Connectivity = &ampEndpointConnectivity{Mode: c.ConnectivityMode, AllowedCidrs: c.AllowedCidrs}
	}
	if full {
		spec.Role = c.Role
		spec.Password = c.Password
	}
	return spec
}

// ampEndpointPlanCheck is what the plan-time checks of an endpoint need.
type ampEndpointPlanCheck struct {
	Credentials *AmpCredentials
	// The prior credentials when the endpoint already exists.
	PriorCredentials *AmpCredentials
	Exists           bool
	// Empty when the role is not known yet.
	Role           string
	SuspendTimeout types.Int64
}

// check returns the errors in an endpoint's planned configuration, so they show at plan time
// rather than part way through an apply.
func (c ampEndpointPlanCheck) check() diag.Diagnostics {
	var diags diag.Diagnostics
	if known(c.SuspendTimeout.Null, c.SuspendTimeout.Unknown) {
		diags.AddAttributeError(path.Root("suspend_timeout_seconds"), "Not supported yet",
			"Endpoints cannot suspend when idle yet. Remove suspend_timeout_seconds.")
	}
	password := types.String{Null: true}
	if c.Credentials != nil {
		creds := c.Credentials
		if !creds.PasswordWo.Null || !creds.PasswordWoVersion.Null {
			diags.AddAttributeError(path.Root("credentials").AtName("password_wo"), "Not supported yet",
				"Write-only passwords and password rotation are not supported yet. Set credentials.password instead.")
		}
		if known(creds.Username.Null, creds.Username.Unknown) && creds.Username.Value != ampUsername {
			diags.AddAttributeError(path.Root("credentials").AtName("username"), "Not supported yet",
				fmt.Sprintf("Only the %s user is supported for now.", ampUsername))
		}
		password = creds.Password
	}
	if c.Role == ampRoleReadOnly && !password.Null {
		diags.AddAttributeError(path.Root("credentials"), "Unexpected password",
			"A read_only endpoint does not take a password; it uses the cluster's.")
	}
	if c.Exists && !password.Unknown {
		prior := types.String{Null: true}
		if c.PriorCredentials != nil {
			prior = c.PriorCredentials.Password
		}
		switch {
		case prior.Null && !password.Null:
			// An imported endpoint has no password in state, since it is never read back.
			diags.AddAttributeWarning(path.Root("credentials").AtName("password"), "Password is only recorded",
				"The password of an existing endpoint cannot be set, so this password is only stored in state. "+
					"Make sure it is the endpoint's current password.")
		case !prior.Equal(password):
			diags.AddAttributeError(path.Root("credentials").AtName("password"), "Password cannot be changed",
				"Passwords cannot be rotated yet, so the password cannot be changed or removed after creation.")
		}
	}
	return diags
}

// ampPlanCheckFrom reads the attributes an endpoint's plan-time checks need. Values that are not
// known yet are skipped; Terraform plans again with them known before it applies.
func ampPlanCheckFrom(ctx context.Context, req tfsdk.ModifyResourcePlanRequest, role string) (ampEndpointPlanCheck, bool) {
	c := ampEndpointPlanCheck{Role: role, Exists: !req.State.Raw.IsNull()}
	if req.Plan.Raw.IsNull() {
		return c, false
	}
	if diags := req.Plan.GetAttribute(ctx, path.Root("credentials"), &c.Credentials); diags.HasError() {
		return c, false
	}
	if diags := req.Plan.GetAttribute(ctx, path.Root("suspend_timeout_seconds"), &c.SuspendTimeout); diags.HasError() {
		return c, false
	}
	if c.Exists {
		if diags := req.State.GetAttribute(ctx, path.Root("credentials"), &c.PriorCredentials); diags.HasError() {
			return c, false
		}
	}
	return c, true
}

// checkExtensionsKept returns an error for each extension the plan removes from a branch, which
// AMP cannot do.
func checkExtensionsKept(ctx context.Context, req tfsdk.ModifyResourcePlanRequest, attribute string) diag.Diagnostics {
	var diags diag.Diagnostics
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return diags
	}
	var planned, prior types.Set
	diags.Append(req.Plan.GetAttribute(ctx, path.Root(attribute), &planned)...)
	diags.Append(req.State.GetAttribute(ctx, path.Root(attribute), &prior)...)
	if diags.HasError() || planned.Unknown {
		return diags
	}
	for _, old := range prior.Elems {
		if !setContains(planned, old) {
			diags.AddAttributeError(path.Root(attribute), "Extensions cannot be removed",
				fmt.Sprintf("Extension %s cannot be removed from a branch; extensions can only be added.", old.(types.String).Value))
		}
	}
	return diags
}

func setContains(s types.Set, v attr.Value) bool {
	for _, e := range s.Elems {
		if e.Equal(v) {
			return true
		}
	}
	return false
}

// modifyAmpClusterPlan runs the plan-time checks of a cluster resource.
func modifyAmpClusterPlan(ctx context.Context, req tfsdk.ModifyResourcePlanRequest, resp *tfsdk.ModifyResourcePlanResponse) {
	resp.Diagnostics.Append(checkExtensionsKept(ctx, req, "branch_extensions")...)
}

// ampEndpointStatus is what the provider reads back for an endpoint: its configuration from the
// desired state and its status and connection details from the observed state.
type ampEndpointStatus struct {
	Config           ampEndpointConfig
	EndpointID       string
	State            string
	Host             string
	Port             string
	TlsCaCertificate string
}

func readAmpEndpoint(desired []ampResource, observed []ampObserved, path string) (ampEndpointStatus, bool, error) {
	var s ampEndpointStatus
	res, ok := findAmpDesired(desired, ampKindEndpoint, path)
	if !ok {
		return s, false, nil
	}
	if len(res.Spec) > 0 {
		var es ampEndpointSpec
		if err := json.Unmarshal(res.Spec, &es); err != nil {
			return s, true, fmt.Errorf("unable to parse the spec of endpoint %s: %v", path, err)
		}
		s.Config.Role = es.Role
		if es.Resources != nil {
			s.Config.Cpu = es.Resources.CPU
			s.Config.Memory = es.Resources.Memory
		}
		if es.Connectivity != nil {
			s.Config.ConnectivityMode = es.Connectivity.Mode
			s.Config.AllowedCidrs = es.Connectivity.AllowedCidrs
		}
		s.Config.PgSettings = es.PgSettings
	}
	s.State = "creating"
	if o, ok := findAmpObserved(observed, ampKindEndpoint, path); ok {
		s.EndpointID = o.Attrs["id"]
		s.State = o.State
		s.Host, s.Port = splitHostPort(o.Attrs["address"])
		s.TlsCaCertificate = o.Attrs["tls_ca_crt"]
		// What AMP applied fills in what the desired spec left to it.
		if s.Config.Role == "" {
			s.Config.Role = o.Attrs["role"]
		}
		if s.Config.Cpu == "" {
			s.Config.Cpu = o.Attrs["cpu"]
		}
		if s.Config.Memory == "" {
			s.Config.Memory = o.Attrs["memory"]
		}
		if s.Config.ConnectivityMode == "" {
			s.Config.ConnectivityMode = o.Attrs["connectivity_mode"]
		}
	}
	if s.Config.Role == "" {
		s.Config.Role = ampRoleReadOnly
	}
	return s, true, nil
}

// ampClusterStatus is what the provider reads back for a cluster and its main branch.
type ampClusterStatus struct {
	Config    ampClusterConfig
	ClusterID string
	State     string
}

// readAmpCluster finds the cluster in a storage pool's desired and observed state.
func readAmpCluster(desired []ampResource, observed []ampObserved, pool, cluster string) (ampClusterStatus, bool, error) {
	var s ampClusterStatus
	clusterRes, ok := findAmpDesired(desired, ampKindCluster, ampClusterName(pool, cluster))
	if !ok {
		return s, false, nil
	}
	var cs ampClusterSpec
	if len(clusterRes.Spec) > 0 {
		if err := json.Unmarshal(clusterRes.Spec, &cs); err != nil {
			return s, true, fmt.Errorf("unable to parse the spec of cluster %s: %v", cluster, err)
		}
	}
	s.Config.PgVersion = cs.PgVersion
	s.Config.Engine = cs.Engine
	s.Config.Extensions = cs.Extensions
	s.State = "creating"
	if o, ok := findAmpObserved(observed, ampKindCluster, ampClusterName(pool, cluster)); ok {
		s.ClusterID = o.Attrs["id"]
		s.State = o.State
		// What AMP applied fills in what the desired spec left to it.
		if s.Config.PgVersion == 0 {
			s.Config.PgVersion, _ = strconv.ParseInt(o.Attrs["pg_version"], 10, 64)
		}
		if s.Config.Engine == "" {
			s.Config.Engine = o.Attrs["engine"]
		}
	}
	if main, ok := findAmpDesired(desired, ampKindBranch, ampBranchName(pool, cluster, ampDefaultBranch)); ok && len(main.Spec) > 0 {
		var bs ampBranchSpec
		if err := json.Unmarshal(main.Spec, &bs); err != nil {
			return s, true, fmt.Errorf("unable to parse the spec of the main branch of cluster %s: %v", cluster, err)
		}
		s.Config.BranchPgSettings = bs.PgSettings
		s.Config.BranchExtensions = bs.Extensions
	}
	return s, true, nil
}

func derefMap(m *map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	return *m
}

func derefStrings(s *[]string) []string {
	if s == nil {
		return nil
	}
	return *s
}

func tfInt64(v int64) types.Int64 {
	if v == 0 {
		return types.Int64{Null: true}
	}
	return types.Int64{Value: v}
}

// ampCreateClusterItems are the resources a cluster resource creates: the cluster and its main
// branch.
func ampCreateClusterItems(pool, clusterName string, config ampClusterConfig) []ampPatchItem {
	return []ampPatchItem{
		{Kind: ampKindCluster, Name: ampClusterName(pool, clusterName), Spec: config.clusterSpec()},
		{Kind: ampKindBranch, Name: ampBranchName(pool, clusterName, ampDefaultBranch), Spec: config.mainBranchSpec(false)},
	}
}

func ampClusterTargets(pool, clusterName string) []ampTarget {
	return []ampTarget{
		{Kind: ampKindCluster, Name: ampClusterName(pool, clusterName)},
		{Kind: ampKindBranch, Name: ampBranchName(pool, clusterName, ampDefaultBranch)},
	}
}

// createAmpCluster creates a cluster with its main branch, then adds the main branch's
// extensions, which a new branch does not take.
func createAmpCluster(ctx context.Context, scope ampScope, region, clusterName string, config ampClusterConfig) (created bool, err error) {
	pool := scope.pool()
	if err := scope.apply(ctx, region, ampIntentCreate, ampCreateClusterItems(pool, clusterName, config)); err != nil {
		return false, err
	}
	if err := waitForAmp(ctx, scope, ampClusterTargets(pool, clusterName)); err != nil {
		return true, fmt.Errorf("the cluster did not become ready: %v", err)
	}
	if len(config.BranchExtensions) > 0 {
		items := []ampPatchItem{{Kind: ampKindBranch, Name: ampBranchName(pool, clusterName, ampDefaultBranch),
			Spec: ampBranchSpec{Extensions: config.BranchExtensions}}}
		if err := scope.apply(ctx, region, ampIntentUpdate, items); err != nil {
			return true, fmt.Errorf("unable to add the main branch's extensions: %v", err)
		}
		if err := waitForAmp(ctx, scope, ampClusterTargets(pool, clusterName)); err != nil {
			return true, fmt.Errorf("the main branch's extensions were not added: %v", err)
		}
	}
	return true, nil
}

// updateAmpCluster applies the in-place changes of a cluster resource, which are all its main
// branch's: every cluster-level attribute forces a replacement.
func updateAmpCluster(ctx context.Context, scope ampScope, region, clusterName string, config ampClusterConfig) error {
	mainPath := ampBranchName(scope.pool(), clusterName, ampDefaultBranch)
	snap, err := scope.snapshot(ctx)
	if err != nil {
		return err
	}
	// A cluster created outside Terraform may not declare its main branch.
	if _, ok := ampEnvDeclaring(snap, ampKindBranch, mainPath); !ok {
		if err := scope.apply(ctx, region, ampIntentCreate, []ampPatchItem{{Kind: ampKindBranch, Name: mainPath, Spec: config.mainBranchSpec(false)}}); err != nil {
			return err
		}
		if err := waitForAmp(ctx, scope, []ampTarget{{Kind: ampKindBranch, Name: mainPath}}); err != nil {
			return err
		}
	}
	spec := config.mainBranchSpec(true)
	if spec.PgSettings == nil && spec.Extensions == nil {
		return nil
	}
	return scope.apply(ctx, region, ampIntentUpdate, []ampPatchItem{{Kind: ampKindBranch, Name: mainPath, Spec: spec}})
}

// deleteAmpCluster deletes a cluster with its branches and endpoints. Unless forced, it refuses
// while the cluster has endpoints or branches besides main, so Terraform state never keeps
// resources the deletion took with it.
func deleteAmpCluster(ctx context.Context, scope ampScope, region, clusterName string, force bool) error {
	pool := scope.pool()
	clusterPath := ampClusterName(pool, clusterName)
	snap, err := scope.snapshot(ctx)
	if err != nil {
		return err
	}
	env, ok := ampEnvDeclaring(snap, ampKindCluster, clusterPath)
	if !ok {
		return nil
	}
	if !force {
		owned := map[string]bool{
			ampKindCluster + " " + clusterPath:                                       true,
			ampKindBranch + " " + ampBranchName(pool, clusterName, ampDefaultBranch): true,
		}
		if dependents := ampDependents(env, []string{clusterPath}, owned); len(dependents) > 0 {
			return fmt.Errorf("cluster %s still has %s. Delete them first, or set force_destroy = true and apply "+
				"before destroying the cluster to delete them with it", clusterName, strings.Join(dependents, ", "))
		}
	}
	if err := scope.apply(ctx, region, ampIntentDelete, []ampPatchItem{{Kind: ampKindCluster, Name: clusterPath}}); err != nil && !isYcpNotFound(err) {
		return err
	}
	return waitForAmpGone(ctx, scope, []ampTarget{{Kind: ampKindCluster, Name: clusterPath}})
}

func tfString(v string) types.String {
	if v == "" {
		return types.String{Null: true}
	}
	return types.String{Value: v}
}
