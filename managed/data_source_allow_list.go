package managed

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

func (r *dataSourceAllowList) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: `The data source to fetch an allow list in YugabyteDB Aeon.`,
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account this allow list belongs to.",
				Computed:    true,
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project this allow list belongs to.",
				Computed:    true,
			},
			"allow_list_id": schema.StringAttribute{
				Description: "The ID of the allow list. Created automatically when an allow list is created. Use this ID to get a specific allow list.",
				Computed:    true,
			},
			"allow_list_name": schema.StringAttribute{
				Description: "The name of the allow list.",
				Required:    true,
			},
			"allow_list_description": schema.StringAttribute{
				Description: "The description of the allow list.",
				Computed:    true,
			},
			"cidr_list": schema.SetAttribute{
				Description: "The CIDR list of the allow list.",
				ElementType: types.StringType,
				Computed:    true,
			},
			"cluster_ids": schema.SetAttribute{
				Description: "List of the IDs of the clusters the allow list is assigned to.",
				ElementType: types.StringType,
				Computed:    true,
			},
		},
	}
}

func newDataSourceAllowList() datasource.DataSource {
	return &dataSourceAllowList{}
}

func (r *dataSourceAllowList) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_allow_list"
}

func (r *dataSourceAllowList) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type dataSourceAllowList struct {
	p provider
}

func (r dataSourceAllowList) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var config AllowList
	var accountId, message string
	var getAccountOK bool
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient := r.p.client

	accountId, getAccountOK, message = getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get the project ID ", message)
		return
	}

	allowList, readOK, message := resourceAllowListRead(accountId, projectId, config.AllowListName.ValueString(), apiClient)
	if !readOK {
		resp.Diagnostics.AddError(fmt.Sprintf("Unable to fetch allow list %s", config.AllowListName.ValueString()), message)
		return
	}

	tflog.Debug(ctx, "Allow List Read: Allow list on read from API server", map[string]interface{}{
		"Allow List": allowList})

	diags = resp.State.Set(ctx, &allowList)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}
