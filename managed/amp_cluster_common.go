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
	ampDefaultPgVersion        = 18
	ampDefaultEngine           = "postgres"
	ampDefaultCpu              = "2"
	ampDefaultConnectivityMode = "cluster_ip"

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

var ampEngines = []string{"postgres", "aidb", "oracle", "sqlserver", "documentdb"}
var ampConnectivityModes = []string{"cluster_ip", "ingress_internal", "ingress_external", "nlb_internal", "nlb_external"}

const ampPgSettingsReset = "Removing a key resets the setting: an endpoint setting falls back to its branch's value, and a branch setting to the PostgreSQL default."

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
		Description: description + " Can be changed in place. " + ampPgSettingsReset,
		Type:        types.MapType{ElemType: types.StringType},
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

func ampCredentialsAttribute(description string, required bool) tfsdk.Attribute {
	return tfsdk.Attribute{
		Description: description,
		Required:    required,
		Optional:    !required,
		Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
			"username": {
				Description: "The user. Only the default, " + ampUsername + ", is supported for now.",
				Type:        types.StringType,
				Optional:    true,
			},
			"password": {
				Description: "The password, 8 to 128 printable ASCII characters. It cannot be changed yet, since AMP cannot rotate passwords.",
				Type:        types.StringType,
				Optional:    true,
				Sensitive:   true,
				Validators:  []tfsdk.AttributeValidator{stringvalidator.LengthBetween(8, 128)},
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

// ampEndpointAttributes are the attributes of an endpoint: the cluster resources' read-write
// endpoint and the ybm_amp_endpoint resource.
func ampEndpointAttributes() map[string]tfsdk.Attribute {
	return map[string]tfsdk.Attribute{
		"cpu": {
			Description:   "The CPU of the endpoint as a Kubernetes quantity, such as 2 or 500m. Defaults to 2. Can be changed in place to resize the endpoint.",
			Type:          types.StringType,
			Optional:      true,
			Computed:      true,
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
		},
		"memory": {
			Description: "The memory of the endpoint as a Kubernetes quantity in Mi or larger units, such as 16Gi. " +
				"Must be at least 8Gi per vCPU. Defaults to 8Gi per vCPU. Can be changed in place to resize the endpoint.",
			Type:     types.StringType,
			Optional: true,
			Computed: true,
		},
		"suspend_timeout_seconds": {
			Description: "Reserved for suspending the endpoint after this many idle seconds. Not supported yet.",
			Type:        types.Int64Type,
			Optional:    true,
		},
		"connectivity_mode": {
			Description:   "How the endpoint is exposed: " + strings.Join(ampConnectivityModes, ", ") + ". Defaults to cluster_ip. Changing it forces a replacement.",
			Type:          types.StringType,
			Optional:      true,
			Computed:      true,
			Validators:    []tfsdk.AttributeValidator{stringvalidator.OneOf(ampConnectivityModes...)},
			PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown(), tfsdk.RequiresReplace()},
		},
		"allowed_cidrs": {
			Description: "CIDR blocks allowed to connect to the endpoint.",
			Type:        types.ListType{ElemType: types.StringType},
			Optional:    true,
		},
		"state": {
			Description: "The state the endpoint last reported, such as ready or provisioning.",
			Type:        types.StringType,
			Computed:    true,
		},
		"host": {
			Description: "The host of the endpoint.",
			Type:        types.StringType,
			Computed:    true,
		},
		"port": {
			Description: "The port of the endpoint.",
			Type:        types.StringType,
			Computed:    true,
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
			Description: "The CA certificate to verify the endpoint's TLS certificate with, when the endpoint requires one.",
			Type:        types.StringType,
			Computed:    true,
		},
	}
}

// ampClusterAttributes are the attributes shared by the shared (AMP Managed) and BYOC cluster
// resources: an AMP cluster, its main branch, and the read-write endpoint the provider creates on it.
func ampClusterAttributes() map[string]tfsdk.Attribute {
	attributes := ampEndpointAttributes()
	attributes["credentials"] = ampCredentialsAttribute("The credentials of the read-write endpoint. password is required.", true)
	attributes["account_id"] = tfsdk.Attribute{
		Description:   "The ID of the account this cluster belongs to.",
		Type:          types.StringType,
		Computed:      true,
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
	}
	attributes["cluster_name"] = ampNameAttribute("The name of the cluster. Must be unique within its storage pool. Changing it forces a new cluster.")
	attributes["pg_version"] = tfsdk.Attribute{
		Description:   fmt.Sprintf("The PostgreSQL major version. Defaults to %d. Changing it forces a new cluster.", ampDefaultPgVersion),
		Type:          types.Int64Type,
		Optional:      true,
		Computed:      true,
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown(), tfsdk.RequiresReplace()},
	}
	attributes["engine"] = tfsdk.Attribute{
		Description:   "The database engine: " + strings.Join(ampEngines, ", ") + ". Defaults to postgres. Changing it forces a new cluster.",
		Type:          types.StringType,
		Optional:      true,
		Computed:      true,
		Validators:    []tfsdk.AttributeValidator{stringvalidator.OneOf(ampEngines...)},
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown(), tfsdk.RequiresReplace()},
	}
	attributes["extensions"] = tfsdk.Attribute{
		Description:   "Extensions to install in the cluster. Changing it forces a new cluster; use ybm_amp_branch to add extensions to a branch in place.",
		Type:          types.ListType{ElemType: types.StringType},
		Optional:      true,
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.RequiresReplace()},
	}
	attributes["cluster_id"] = tfsdk.Attribute{
		Description:   "The ID of the cluster.",
		Type:          types.StringType,
		Computed:      true,
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
	}
	attributes["branch_pg_settings"] = ampPgSettingsAttribute(
		"PostgreSQL settings of the main branch, such as work_mem. They apply to every endpoint on main, including those of ybm_amp_endpoint.")
	attributes["endpoint_pg_settings"] = ampPgSettingsAttribute(
		"PostgreSQL settings of the read-write endpoint only. A key set here overrides the same key in branch_pg_settings.")
	attributes["force_destroy"] = ampForceDestroyAttribute(
		"Whether destroying or replacing the cluster may also delete branches and endpoints that are not managed by this resource. " +
			"Without it, deleting a cluster that still has them fails.")
	return attributes
}

// ampEndpointConfig is the configuration of an endpoint, with defaults applied.
type ampEndpointConfig struct {
	Role             string
	Password         string
	Cpu              string
	Memory           string
	ConnectivityMode string
	AllowedCidrs     []string
	PgSettings       map[string]string
}

// ampClusterConfig is the configuration of an AMP cluster with its main branch and read-write
// endpoint.
type ampClusterConfig struct {
	PgVersion        int64
	Engine           string
	Extensions       []string
	BranchPgSettings map[string]string
	Endpoint         ampEndpointConfig
}

func ampPassword(c *AmpCredentials) string {
	if c == nil {
		return ""
	}
	return c.Password.Value
}

func newAmpEndpointConfig(role string, credentials *AmpCredentials, cpu types.String, memory types.String,
	mode types.String, allowedCidrs []types.String, pgSettings types.Map) (ampEndpointConfig, error) {
	c := ampEndpointConfig{
		Role:             role,
		Password:         ampPassword(credentials),
		Cpu:              ampDefaultCpu,
		ConnectivityMode: ampDefaultConnectivityMode,
		AllowedCidrs:     stringsFromTf(allowedCidrs),
		PgSettings:       mapFromTf(pgSettings),
	}
	if known(cpu.Null, cpu.Unknown) {
		c.Cpu = cpu.Value
	}
	if known(mode.Null, mode.Unknown) {
		c.ConnectivityMode = mode.Value
	}
	if known(memory.Null, memory.Unknown) {
		c.Memory = memory.Value
	} else {
		// AMP wants cpu and memory declared together on a resize, so an unset memory is derived
		// from the cpu the same way the server defaults it.
		m, err := ampDefaultMemory(c.Cpu)
		if err != nil {
			return c, err
		}
		c.Memory = m
	}
	return c, nil
}

func newAmpClusterConfig(pgVersion types.Int64, engine types.String, extensions []types.String, credentials *AmpCredentials,
	cpu types.String, memory types.String, mode types.String, allowedCidrs []types.String, branchPgSettings types.Map,
	endpointPgSettings types.Map) (ampClusterConfig, error) {
	c := ampClusterConfig{
		PgVersion:        ampDefaultPgVersion,
		Engine:           ampDefaultEngine,
		Extensions:       stringsFromTf(extensions),
		BranchPgSettings: mapFromTf(branchPgSettings),
	}
	if known(pgVersion.Null, pgVersion.Unknown) {
		c.PgVersion = pgVersion.Value
	}
	if known(engine.Null, engine.Unknown) {
		c.Engine = engine.Value
	}
	endpoint, err := newAmpEndpointConfig(ampRoleReadWrite, credentials, cpu, memory, mode, allowedCidrs, endpointPgSettings)
	c.Endpoint = endpoint
	return c, err
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

// stringsToTf returns the list to store. An empty list is stored as configured, empty or null, so
// the read-back matches the plan.
func stringsToTf(values []string, prior []types.String) []types.String {
	if len(values) == 0 {
		if prior != nil {
			return []types.String{}
		}
		return nil
	}
	out := make([]types.String, 0, len(values))
	for _, v := range values {
		out = append(out, types.String{Value: v})
	}
	return out
}

// mapFromTf returns the map's values, empty rather than nil so an emptied map is still sent.
func mapFromTf(m types.Map) map[string]string {
	out := map[string]string{}
	if m.Null || m.Unknown {
		return out
	}
	for k, v := range m.Elems {
		if s, ok := v.(types.String); ok {
			out[k] = s.Value
		}
	}
	return out
}

// mapToTf returns the map to store. An empty map is stored as configured, empty or null, so the
// read-back matches the plan.
func mapToTf(m map[string]string, prior types.Map) types.Map {
	if len(m) == 0 && (prior.Null || prior.Unknown) {
		return types.Map{ElemType: types.StringType, Null: true}
	}
	elems := make(map[string]attr.Value, len(m))
	for k, v := range m {
		elems[k] = types.String{Value: v}
	}
	return types.Map{ElemType: types.StringType, Elems: elems}
}

// ampCredentialsToTf returns the credentials to store given the password in the desired spec.
func ampCredentialsToTf(password string, prior *AmpCredentials) *AmpCredentials {
	if prior == nil && password == "" {
		return nil
	}
	c := &AmpCredentials{
		Username:          types.String{Null: true},
		Password:          tfString(password),
		PasswordWo:        types.String{Null: true},
		PasswordWoVersion: types.Int64{Null: true},
	}
	if prior != nil {
		if known(prior.Username.Null, prior.Username.Unknown) {
			c.Username = prior.Username
		}
		// The server echoes the password today; keep the configured one if it ever stops.
		if c.Password.Null && known(prior.Password.Null, prior.Password.Unknown) {
			c.Password = prior.Password
		}
	}
	return c
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

func (c ampClusterConfig) mainBranchSpec() ampBranchSpec {
	pg := c.BranchPgSettings
	if pg == nil {
		pg = map[string]string{}
	}
	return ampBranchSpec{PgSettings: &pg}
}

// spec returns the endpoint's spec. A full spec carries the create-time fields; otherwise only the
// fields AMP can change in place are sent. Nested maps are always sent whole, since an update
// replaces them rather than merging them.
func (c ampEndpointConfig) spec(full bool) ampEndpointSpec {
	cidrs := c.AllowedCidrs
	if cidrs == nil {
		cidrs = []string{}
	}
	pg := c.PgSettings
	if pg == nil {
		pg = map[string]string{}
	}
	spec := ampEndpointSpec{
		Resources:    &ampEndpointResources{CPU: c.Cpu, Memory: c.Memory},
		Connectivity: &ampEndpointConnectivity{Mode: c.ConnectivityMode, AllowedCidrs: cidrs},
		PgSettings:   &pg,
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
			"AMP endpoints cannot suspend when idle yet. Remove suspend_timeout_seconds.")
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
	switch c.Role {
	case ampRoleReadWrite:
		if password.Null {
			diags.AddAttributeError(path.Root("credentials").AtName("password"), "Missing password",
				"A read_write endpoint requires credentials.password.")
		}
	case ampRoleReadOnly:
		if !password.Null {
			diags.AddAttributeError(path.Root("credentials"), "Unexpected password",
				"A read_only endpoint does not take a password; it uses the read_write endpoint's.")
		}
	}
	if c.Exists && !password.Unknown {
		prior := types.String{Null: true}
		if c.PriorCredentials != nil {
			prior = c.PriorCredentials.Password
		}
		if !prior.Equal(password) {
			diags.AddAttributeError(path.Root("credentials").AtName("password"), "Password cannot be changed",
				"AMP cannot rotate passwords yet, so the password cannot be changed, added or removed after creation.")
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

// modifyAmpClusterPlan runs the plan-time checks of a cluster resource's read-write endpoint.
func modifyAmpClusterPlan(ctx context.Context, req tfsdk.ModifyResourcePlanRequest, resp *tfsdk.ModifyResourcePlanResponse) {
	if c, ok := ampPlanCheckFrom(ctx, req, ampRoleReadWrite); ok {
		resp.Diagnostics.Append(c.check()...)
	}
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
		s.Config.Password = es.Password
		if es.Resources != nil {
			s.Config.Cpu = es.Resources.CPU
			s.Config.Memory = es.Resources.Memory
		}
		if es.Connectivity != nil {
			s.Config.ConnectivityMode = es.Connectivity.Mode
			s.Config.AllowedCidrs = es.Connectivity.AllowedCidrs
		}
		if es.PgSettings != nil {
			s.Config.PgSettings = *es.PgSettings
		}
	}
	if s.Config.Role == "" {
		s.Config.Role = ampRoleReadWrite
	}
	if o, ok := findAmpObserved(observed, ampKindEndpoint, path); ok {
		s.EndpointID = o.Attrs["id"]
		s.State = o.State
		s.Host, s.Port = splitHostPort(o.Attrs["address"])
		s.TlsCaCertificate = o.Attrs["tls_ca_crt"]
		// The observed size is what AMP applied, so it fills in a desired spec that omitted it.
		if s.Config.Cpu == "" {
			s.Config.Cpu = o.Attrs["cpu"]
		}
		if s.Config.Memory == "" {
			s.Config.Memory = o.Attrs["memory"]
		}
		if s.Config.ConnectivityMode == "" {
			s.Config.ConnectivityMode = o.Attrs["connectivity_mode"]
		}
	} else {
		s.State = "creating"
	}
	return s, true, nil
}

// ampClusterStatus is what the provider reads back for a cluster, its main branch and its
// read-write endpoint.
type ampClusterStatus struct {
	Config    ampClusterConfig
	ClusterID string
	Endpoint  ampEndpointStatus
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
	if s.Config.PgVersion == 0 {
		s.Config.PgVersion = ampDefaultPgVersion
	}
	s.Config.Engine = cs.Engine
	if s.Config.Engine == "" {
		s.Config.Engine = ampDefaultEngine
	}
	s.Config.Extensions = cs.Extensions
	if o, ok := findAmpObserved(observed, ampKindCluster, ampClusterName(pool, cluster)); ok {
		s.ClusterID = o.Attrs["id"]
	}
	if main, ok := findAmpDesired(desired, ampKindBranch, ampBranchName(pool, cluster, ampDefaultBranch)); ok && len(main.Spec) > 0 {
		var bs ampBranchSpec
		if err := json.Unmarshal(main.Spec, &bs); err != nil {
			return s, true, fmt.Errorf("unable to parse the spec of the main branch of cluster %s: %v", cluster, err)
		}
		if bs.PgSettings != nil {
			s.Config.BranchPgSettings = *bs.PgSettings
		}
	}

	endpoint, ok, err := readAmpEndpoint(desired, observed, ampEndpointName(pool, cluster))
	if err != nil {
		return s, true, err
	}
	if !ok {
		endpoint.State = "creating"
	}
	s.Endpoint = endpoint
	s.Config.Endpoint = endpoint.Config
	return s, true, nil
}

// ampCreateClusterItems are the resources a cluster resource creates: the cluster, its main
// branch, and the read-write endpoint on main.
func ampCreateClusterItems(pool, clusterName string, config ampClusterConfig) []ampPatchItem {
	return []ampPatchItem{
		{Kind: ampKindCluster, Name: ampClusterName(pool, clusterName), Spec: config.clusterSpec()},
		{Kind: ampKindBranch, Name: ampBranchName(pool, clusterName, ampDefaultBranch), Spec: config.mainBranchSpec()},
		{Kind: ampKindEndpoint, Name: ampEndpointName(pool, clusterName), Spec: config.Endpoint.spec(true)},
	}
}

func ampClusterTargets(pool, clusterName string) []ampTarget {
	return []ampTarget{
		{Kind: ampKindCluster, Name: ampClusterName(pool, clusterName)},
		{Kind: ampKindBranch, Name: ampBranchName(pool, clusterName, ampDefaultBranch)},
		{Kind: ampKindEndpoint, Name: ampEndpointName(pool, clusterName)},
	}
}

// updateAmpCluster applies the in-place changes of a cluster resource: the main branch's and the
// read-write endpoint's. Every cluster-level attribute forces a replacement.
func updateAmpCluster(ctx context.Context, scope ampScope, region, clusterName string, config ampClusterConfig) error {
	pool := scope.pool()
	mainPath := ampBranchName(pool, clusterName, ampDefaultBranch)
	snap, err := scope.snapshot(ctx)
	if err != nil {
		return err
	}
	// A cluster created outside Terraform may not declare its main branch.
	mainIntent := ampIntentUpdate
	if _, ok := ampEnvDeclaring(snap, ampKindBranch, mainPath); !ok {
		mainIntent = ampIntentCreate
	}
	if err := scope.apply(ctx, region, mainIntent, []ampPatchItem{{Kind: ampKindBranch, Name: mainPath, Spec: config.mainBranchSpec()}}); err != nil {
		return err
	}
	items := []ampPatchItem{{Kind: ampKindEndpoint, Name: ampEndpointName(pool, clusterName), Spec: config.Endpoint.spec(false)}}
	return scope.apply(ctx, region, ampIntentUpdate, items)
}

// deleteAmpCluster deletes a cluster with its branches and endpoints. Unless forced, it refuses
// while the cluster has branches or endpoints besides the main branch and read-write endpoint
// the cluster resource owns, so Terraform state never keeps resources the deletion took with it.
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
			ampKindEndpoint + " " + ampEndpointName(pool, clusterName):               true,
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
