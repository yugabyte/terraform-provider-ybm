/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/yugabyte/terraform-provider-ybm/managed/fflags"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

var stderr = os.Stderr

func New(version string) func() tfsdk.Provider {
	return func() tfsdk.Provider {
		return &provider{
			version: version,
		}
	}
}

type provider struct {
	version    string
	configured bool
	client     *openapiclient.APIClient
}

func (p *provider) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: `[YugabyteDB](https://github.com/yugabyte/yugabyte-db) is a high-performance, cloud-native distributed SQL database that aims to support all PostgreSQL
features. It is best to fit for cloud-native OLTP (i.e. real-time, business-critical) applications that need absolute
data correctness and require at least one of the following: scalability, high tolerance to failures, or
globally-distributed deployments. [YugabyteDB Aeon](https://www.yugabyte.com/managed/) is a fully managed YugabyteDB-as-a-Service without
the operational overhead of managing a database.  

The YugabyteDB Aeon Provider can be used to interact with the resources provided by YugabyteDB Aeon like the YugabyteDB Clusters, Allow lists, VPCs,
VPC Peerings, Read Replicas and so on. The provider needs to be configured with appropriate credentials before it can base used. The navigation bar on the left
hand side provides the details about all the resources supported by the provider and the guides to use the provider.`,
		Attributes: map[string]tfsdk.Attribute{
			"auth_token": {
				Description: "The authentication token (API key) of the account this cluster belongs to.",
				Type:        types.StringType,
				Required:    true,
				Sensitive:   true,
			},
			"host": {
				Description: "The environment this cluster is being created in, for example, cloud.yugabyte.com ",
				Type:        types.StringType,
				Required:    true,
			},
			"use_secure_host": {
				Description: "Set to true to use a secure connection (HTTPS) to the host.",
				Type:        types.BoolType,
				Optional:    true,
			},
		},
	}, nil
}

type providerData struct {
	AuthToken     types.String `tfsdk:"auth_token"`
	Host          types.String `tfsdk:"host"`
	UseSecureHost types.Bool   `tfsdk:"use_secure_host"`
}

func (p *provider) Configure(ctx context.Context, req tfsdk.ConfigureProviderRequest, resp *tfsdk.ConfigureProviderResponse) {
	// Retrieve provider data from configuration
	var config providerData
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var auth_token string
	if config.AuthToken.Unknown {
		resp.Diagnostics.AddWarning(
			"Unable to create client", "The authentication token is invalid.",
		)
		return
	}
	if config.AuthToken.Null {
		auth_token = os.Getenv("YB_AUTH_TOKEN")
	} else {
		auth_token = config.AuthToken.Value
	}
	if auth_token == "" {
		resp.Diagnostics.AddError(
			"Missing authentication token", "You must provide an authentication token.",
		)
	}

	var host string
	if config.Host.Unknown {
		resp.Diagnostics.AddWarning(
			"Unable to create client", "The provided host is not recognized.",
		)
		return
	}
	if config.Host.Null {
		host = os.Getenv("YB_CLOUD_HOST")
	} else {
		host = config.Host.Value
	}
	if host == "" {
		host = "localhost:9000"
	}

	use_secure_host := true
	if config.UseSecureHost.Unknown {
		resp.Diagnostics.AddWarning(
			"Unable to create client", "You must specify use_secure_host; valid values are true or false.",
		)
		return
	}
	if !config.UseSecureHost.Null {
		use_secure_host = config.UseSecureHost.Value
	}

	configuration := openapiclient.NewConfiguration()
	configuration.Host = host
	if use_secure_host {
		configuration.Scheme = "https"
	} else {
		configuration.Scheme = "http"
	}
	api_client := openapiclient.NewAPIClient(configuration)

	// authorize user
	api_client.GetConfig().AddDefaultHeader("Authorization", "Bearer "+auth_token)

	// add client header
	api_client.GetConfig().UserAgent = "terraform-provider-ybm/" + p.version

	p.client = api_client
	p.configured = true
}

func (p *provider) GetResources(_ context.Context) (map[string]tfsdk.ResourceType, diag.Diagnostics) {
	resources := map[string]tfsdk.ResourceType{
		"ybm_cluster":                            resourceClusterType{},
		"ybm_allow_list":                         resourceAllowListType{},
		"ybm_backup":                             resourceBackupType{},
		"ybm_vpc":                                resourceVPCType{},
		"ybm_read_replicas":                      resourceReadReplicasType{},
		"ybm_vpc_peering":                        resourceVPCPeeringType{},
		"ybm_user":                               resourceUserType{},
		"ybm_role":                               resourceRoleType{},
		"ybm_private_service_endpoint":           resourcePrivateEndpointType{},
		"ybm_api_key":                            resourceApiKeyType{},
		"ybm_metrics_exporter":                   resourceMetricsExporterType{},
		"ybm_associate_metrics_exporter_cluster": resourceAssociateMetricsExporterClusterType{},
		"ybm_integration":                        resourceIntegrationType{},
	}

	// Add DB Audit logging resource only if the feature flag is enabled
	if fflags.IsFeatureFlagEnabled(fflags.DB_AUDIT_LOGGING) {
		resources["ybm_associate_db_audit_export_config_cluster"] = resourceAssociateDbAuditExportConfigClusterType{}
	}

	// Add DB Query logging resource only if the feature flag is enabled
	if fflags.IsFeatureFlagEnabled(fflags.DB_QUERY_LOGGING) {
		resources["ybm_database_query_logging"] = resourceDbQueryLoggingType{}
	}

	return resources, nil
}

func (p *provider) GetDataSources(_ context.Context) (map[string]tfsdk.DataSourceType, diag.Diagnostics) {
	dataSources := map[string]tfsdk.DataSourceType{
		"ybm_backup":      dataSourceBackupType{},
		"ybm_cluster":     dataClusterNameType{},
		"ybm_vpc":         dataSourceVPCType{},
		"ybm_allow_list":  dataSourceAllowListType{},
		"ybm_integration": dataSourceIntegrationType{},
	}

	// Add DB Audit logging data source only if the feature flag is enabled
	if fflags.IsFeatureFlagEnabled(fflags.DB_AUDIT_LOGGING) {
		dataSources["ybm_associate_db_audit_export_config_cluster"] = dataSourceAssociateDbAuditExportConfigClusterType{}
	}

	return dataSources, nil
}
