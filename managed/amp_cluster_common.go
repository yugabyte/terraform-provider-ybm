/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/yugabyte/terraform-provider-ybm/managed/plan_modifier"
)

const (
	ampDefaultPgVersion        = 18
	ampDefaultEngine           = "postgres"
	ampDefaultCpu              = "2"
	ampDefaultConnectivityMode = "cluster_ip"

	ampRoleReadWrite = "read_write"
	ampRoleReadOnly  = "read_only"

	ampOperationTimeout = 60 * time.Minute
)

// A var so tests can poll without waiting.
var ampPollInterval = 10 * time.Second

// Names become a segment of the resource path, so they cannot contain a slash.
var ampNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

const ampNameRule = "must be lowercase letters, digits and hyphens, starting with a letter or digit"

var ampEngines = []string{"postgres", "aidb", "oracle", "sqlserver", "documentdb"}
var ampConnectivityModes = []string{"cluster_ip", "ingress_internal", "ingress_external", "nlb_internal", "nlb_external"}

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
		Description: description,
		Type:        types.MapType{ElemType: types.StringType},
		Optional:    true,
	}
}

// ampEndpointAttributes are the attributes of an endpoint: the cluster resources' read-write
// endpoint and the ybm_amp_endpoint resource.
func ampEndpointAttributes(passwordRequired bool) map[string]tfsdk.Attribute {
	password := tfsdk.Attribute{
		Description:   "The password of the cloud_admin user, 8 to 128 printable ASCII characters. It cannot be changed after creation.",
		Type:          types.StringType,
		Sensitive:     true,
		Validators:    []tfsdk.AttributeValidator{stringvalidator.LengthBetween(8, 128)},
		PlanModifiers: []tfsdk.AttributePlanModifier{planmodifier.ImmutableFieldModifier{}},
	}
	if passwordRequired {
		password.Required = true
	} else {
		password.Description += " Required for a read_write endpoint and not allowed for a read_only one."
		password.Optional = true
	}
	return map[string]tfsdk.Attribute{
		"password": password,
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
		"pg_settings": ampPgSettingsAttribute("PostgreSQL settings of the endpoint's compute, such as work_mem. Can be changed in place."),
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
// resources: an AMP cluster plus the read-write endpoint the provider creates on its main branch.
func ampClusterAttributes() map[string]tfsdk.Attribute {
	attributes := ampEndpointAttributes(true)
	attributes["account_id"] = tfsdk.Attribute{
		Description:   "The ID of the account this cluster belongs to.",
		Type:          types.StringType,
		Computed:      true,
		PlanModifiers: []tfsdk.AttributePlanModifier{tfsdk.UseStateForUnknown()},
	}
	attributes["cluster_name"] = ampNameAttribute("The name of the cluster. Must be unique within the account's storage pool. Changing it forces a new cluster.")
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
	// The cluster's endpoint is the read-write one on the main branch, so say so.
	pg := attributes["pg_settings"]
	pg.Description = "PostgreSQL settings of the read-write endpoint's compute, such as work_mem. Can be changed in place."
	attributes["pg_settings"] = pg
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

// ampClusterConfig is the configuration of an AMP cluster with its read-write endpoint.
type ampClusterConfig struct {
	PgVersion  int64
	Engine     string
	Extensions []string
	Endpoint   ampEndpointConfig
}

func newAmpEndpointConfig(role string, password types.String, cpu types.String, memory types.String,
	mode types.String, allowedCidrs []types.String, pgSettings types.Map) (ampEndpointConfig, error) {
	c := ampEndpointConfig{
		Role:             role,
		Password:         password.Value,
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

func newAmpClusterConfig(pgVersion types.Int64, engine types.String, extensions []types.String, password types.String,
	cpu types.String, memory types.String, mode types.String, allowedCidrs []types.String, pgSettings types.Map) (ampClusterConfig, error) {
	c := ampClusterConfig{
		PgVersion:  ampDefaultPgVersion,
		Engine:     ampDefaultEngine,
		Extensions: stringsFromTf(extensions),
	}
	if known(pgVersion.Null, pgVersion.Unknown) {
		c.PgVersion = pgVersion.Value
	}
	if known(engine.Null, engine.Unknown) {
		c.Engine = engine.Value
	}
	endpoint, err := newAmpEndpointConfig(ampRoleReadWrite, password, cpu, memory, mode, allowedCidrs, pgSettings)
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

func stringsToTf(values []string) []types.String {
	if len(values) == 0 {
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

func mapToTf(m map[string]string) types.Map {
	if len(m) == 0 {
		return types.Map{ElemType: types.StringType, Null: true}
	}
	elems := make(map[string]attr.Value, len(m))
	for k, v := range m {
		elems[k] = types.String{Value: v}
	}
	return types.Map{ElemType: types.StringType, Elems: elems}
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

// ampClusterStatus is what the provider reads back for a cluster and its read-write endpoint.
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

// ampCreateClusterItems are the resources a cluster resource creates: the cluster and its
// read-write endpoint on the default branch.
func ampCreateClusterItems(pool, clusterName string, config ampClusterConfig) []ampPatchItem {
	return []ampPatchItem{
		{Kind: ampKindCluster, Name: ampClusterName(pool, clusterName), Spec: config.clusterSpec()},
		{Kind: ampKindEndpoint, Name: ampEndpointName(pool, clusterName), Spec: config.Endpoint.spec(true)},
	}
}

func ampClusterTargets(pool, clusterName string) []ampTarget {
	return []ampTarget{
		{Kind: ampKindCluster, Name: ampClusterName(pool, clusterName)},
		{Kind: ampKindEndpoint, Name: ampEndpointName(pool, clusterName)},
	}
}

func tfString(v string) types.String {
	if v == "" {
		return types.String{Null: true}
	}
	return types.String{Value: v}
}
