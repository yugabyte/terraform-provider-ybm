package managed

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

var stderr = os.Stderr

func New() tfsdk.Provider {
	return &provider{}
}

type provider struct {
	configured bool
	client     *openapiclient.APIClient
}

func (p *provider) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Attributes: map[string]tfsdk.Attribute{
			"auth_token": {
				Description: "The authentication token of the account this cluster belongs to.",
				Type:        types.StringType,
				Optional:    true,
				Sensitive:   true,
			},
			"host": {
				Description: "The environment this cluster is being created in, i.e. devcloud.yugabyte.com, localhost:9000, etc.",
				Type:        types.StringType,
				Optional:    true,
			},
			"use_secure_host": {
				Description: "Whether or not the host requires a secure connection (HTTPS).",
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
			"Unable to create client", "Cannot use unknown value as auth token",
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
			"Unable to find auth token", "Auth token cannot be an empty string",
		)
	}

	var host string
	if config.Host.Unknown {
		resp.Diagnostics.AddWarning(
			"Unable to create client", "Cannot use unknown value as host",
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

	use_secure_host := false
	if config.UseSecureHost.Unknown {
		resp.Diagnostics.AddWarning(
			"Unable to create client", "Cannot use unknown value as use_secure_host",
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
	}
	api_client := openapiclient.NewAPIClient(configuration)

	// authorize user
	api_client.GetConfig().AddDefaultHeader("Authorization", "Bearer "+auth_token)

	p.client = api_client
	p.configured = true
}

func (p *provider) GetResources(_ context.Context) (map[string]tfsdk.ResourceType, diag.Diagnostics) {
	return map[string]tfsdk.ResourceType{
		"ybm_cluster":       resourceClusterType{},
		"ybm_allow_list":    resourceAllowListType{},
		"ybm_backup":        resourceBackupType{},
		"ybm_vpc":           resourceVPCType{},
		"ybm_read_replicas": resourceReadReplicasType{},
	}, nil
}

func (p *provider) GetDataSources(_ context.Context) (map[string]tfsdk.DataSourceType, diag.Diagnostics) {
	return map[string]tfsdk.DataSourceType{
		"ybm_backup":  dataSourceBackupType{},
		"ybm_cluster": dataClusterNameType{},
	}, nil
}
