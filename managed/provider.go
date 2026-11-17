/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/yugabyte/terraform-provider-ybm/managed/fflags"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

var stderr = os.Stderr

func New(version string) func() fwprovider.Provider {
	return func() fwprovider.Provider {
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

func (p *provider) Metadata(_ context.Context, _ fwprovider.MetadataRequest, resp *fwprovider.MetadataResponse) {
	resp.TypeName = "ybm"
	resp.Version = p.version
}

func (p *provider) Schema(_ context.Context, _ fwprovider.SchemaRequest, resp *fwprovider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `[YugabyteDB](https://github.com/yugabyte/yugabyte-db) is a high-performance, cloud-native distributed SQL database that aims to support all PostgreSQL
features. It is best to fit for cloud-native OLTP (i.e. real-time, business-critical) applications that need absolute
data correctness and require at least one of the following: scalability, high tolerance to failures, or
globally-distributed deployments. [YugabyteDB Aeon](https://www.yugabyte.com/managed/) is a fully managed YugabyteDB-as-a-Service without
the operational overhead of managing a database.  

The YugabyteDB Aeon Provider can be used to interact with the resources provided by YugabyteDB Aeon like the YugabyteDB Clusters, Allow lists, VPCs,
VPC Peerings, Read Replicas and so on. The provider needs to be configured with appropriate credentials before it can base used. The navigation bar on the left
hand side provides the details about all the resources supported by the provider and the guides to use the provider.`,
		Attributes: map[string]schema.Attribute{
			"auth_token": schema.StringAttribute{
				Description: "The authentication token (API key) of the account this cluster belongs to.",
				Required:    true,
				Sensitive:   true,
			},
			"host": schema.StringAttribute{
				Description: "The environment this cluster is being created in, for example, cloud.yugabyte.com ",
				Required:    true,
			},
			"use_secure_host": schema.BoolAttribute{
				Description: "Set to true to use a secure connection (HTTPS) to the host.",
				Optional:    true,
			},
		},
	}
}

type providerData struct {
	AuthToken     types.String `tfsdk:"auth_token"`
	Host          types.String `tfsdk:"host"`
	UseSecureHost types.Bool   `tfsdk:"use_secure_host"`
}

func (p *provider) Configure(ctx context.Context, req fwprovider.ConfigureRequest, resp *fwprovider.ConfigureResponse) {
	// Retrieve provider data from configuration
	var config providerData
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var auth_token string
	if config.AuthToken.IsUnknown() {
		resp.Diagnostics.AddWarning(
			"Unable to create client", "The authentication token is invalid.",
		)
		return
	}
	if config.AuthToken.IsNull() {
		auth_token = os.Getenv("YB_AUTH_TOKEN")
	} else {
		auth_token = config.AuthToken.ValueString()
	}
	if auth_token == "" {
		resp.Diagnostics.AddError(
			"Missing authentication token", "You must provide an authentication token.",
		)
	}

	var host string
	if config.Host.IsUnknown() {
		resp.Diagnostics.AddWarning(
			"Unable to create client", "The provided host is not recognized.",
		)
		return
	}
	if config.Host.IsNull() {
		host = os.Getenv("YB_CLOUD_HOST")
	} else {
		host = config.Host.ValueString()
	}
	if host == "" {
		host = "localhost:9000"
	}

	use_secure_host := true
	if config.UseSecureHost.IsUnknown() {
		resp.Diagnostics.AddWarning(
			"Unable to create client", "You must specify use_secure_host; valid values are true or false.",
		)
		return
	}
	if !config.UseSecureHost.IsNull() {
		use_secure_host = config.UseSecureHost.ValueBool()
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
	resp.ResourceData = p
	resp.DataSourceData = p
}

func (p *provider) Resources(_ context.Context) []func() resource.Resource {
	resources := []func() resource.Resource{
		newResourceCluster,
		newResourceAllowList,
		newResourceBackup,
		newResourceBackupRestore,
		newResourceVPC,
		newResourceReadReplicas,
		newResourceVPCPeering,
		newResourceUser,
		newResourceRole,
		newResourcePrivateEndpoint,
		newResourceApiKey,
		newResourceMetricsExporter,
		newResourceAssociateMetricsExporterCluster,
		newResourceIntegration,
		newResourceDbQueryLogging,
		newResourceDbAuditLogging,
		newResourcePitrConfig,
		newResourcePitrClone,
	}

	// Add DR config resource only if the feature flag is enabled
	if fflags.IsFeatureFlagEnabled(fflags.DR) {
		resources = append(resources, newResourceDrConfig)
	}

	// Add autoscaler policy resource only if the feature flag is enabled
	if fflags.IsFeatureFlagEnabled(fflags.Autoscaling) {
		resources = append(resources, newResourceAutoscalerPolicy)
	}

	// Add the Aeon PG resources only if the feature flag is enabled
	if fflags.IsFeatureFlagEnabled(fflags.AMP) {
		resources = append(resources,
			newResourceAmpCluster,
			newResourceByocEnvironment,
			newResourceByocStoragePool,
			newResourceByocCluster,
			newResourceAmpBranch,
			newResourceAmpEndpoint,
		)
	}

	return resources
}

func (p *provider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newDataSourceBackup,
		newDataClusterName,
		newDataSourceVPC,
		newDataSourceAllowList,
		newDataSourceIntegration,
		newDataSourceDbAuditLogging,
	}
}

// configuredProvider returns the provider a resource or data source is configured with. Until the
// provider itself is configured there is none, and the zero provider reports configured == false.
func configuredProvider(providerData any) provider {
	if p, ok := providerData.(*provider); ok {
		return *p
	}
	return provider{}
}
