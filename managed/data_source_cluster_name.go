package managed

import (
	"context"
	"net/http/httputil"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	//"github.com/hashicorp/terraform-plugin-log/tflog"
	//"fmt"
)

type dataClusterNameType struct{}

func (r dataClusterNameType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {

	return tfsdk.Schema{
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this cluster belongs to.",
				Type:        types.StringType,
				Required:    true,
			},
			"project_id": {
				Description: "The ID of the project this cluster belongs to.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},

			"cluster_id": {
				Description: "The id of the cluster. Filled automatically on creating a cluster. Use to get a specific cluster.",
				Type:        types.StringType,
				Optional:    true,
			},
			"cluster_name": {
				Description: "The name of the cluster.",
				Type:        types.StringType,
				Required:    true,
			},
			"cluster_type": {
				Description: "The type of the cluster.",
				Type:        types.StringType,
				Optional:    true,
			},
			"cloud_type": {
				Description: "Which cloud the cluster is deployed in: AWS or GCP. Default GCP.",
				Type:        types.StringType,
				Optional:    true,
			},
			"cluster_region_info": {
				Optional: true,
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
					"num_nodes": {
						Type:     types.Int64Type,
						Optional: true,
					},
					"region": {
						Type:     types.StringType,
						Optional: true,
					},
					"vpc_id": {
						Type:     types.StringType,
						Optional: true,
					},
				}),
			},

			"backup_schedule": {
				Optional: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{

					"state": {

						Description: "The state for  backup schedule. It is use to pause or resume the backup schedule. It can have value ACTIVE or PAUSED only.",
						Type:        types.StringType,
						Optional:    true,
					},

					"cron_expression": {
						Description: "The cron expression for  backup schedule",
						Type:        types.StringType,
						Optional:    true,
					},

					"time_interval_in_days": {
						Description: "The time interval in days for backup schedule.",
						Type:        types.Int64Type,
						Optional:    true,
					},

					"retention_period_in_days": {
						Description: "The retention period of the backup schedule.",
						Type:        types.Int64Type,
						Optional:    true,
					},

					"backup_description": {
						Description: "The description of the backup schedule.",
						Type:        types.StringType,
						Optional:    true,
					},

					"schedule_id": {
						Description: "The id of the backup schedule. Filled automatically on creating a backup schedule. Used to get a specific backup schedule.",
						Type:        types.StringType,
						Optional:    true,
					},
				}),
			},

			"cluster_tier": {
				Description: "FREE or PAID.",
				Type:        types.StringType,
				Optional:    true,
			},
			"fault_tolerance": {
				Description: "The fault tolerance of the cluster.",
				Type:        types.StringType,
				Optional:    true,
			},
			"cluster_allow_list_ids": {
				Description: "The list of IDs of allow lists associated with the cluster.",
				Type: types.ListType{
					ElemType: types.StringType,
				},
				Optional: true,
			},
			"restore_backup_id": {
				Description: "The backup ID to be restored to the cluster.",
				Type:        types.StringType,
				Optional:    true,
			},
			"node_config": {
				Optional: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"num_cores": {
						Type:     types.Int64Type,
						Optional: true,
					},
					"memory_mb": {
						Type:     types.Int64Type,
						Optional: true,
					},
					"disk_size_gb": {
						Type:     types.Int64Type,
						Optional: true,
					},
				}),
			},
			"is_production": {
				Description: "If the cluster is a production cluster. Default false.",
				Type:        types.BoolType,
				Optional:    true,
			},
			"credentials": {
				Optional: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"ysql_username": {
						Type:     types.StringType,
						Optional: true,
					},
					"ysql_password": {
						Type:     types.StringType,
						Optional: true,
					},
					"ycql_username": {
						Type:     types.StringType,
						Optional: true,
					},
					"ycql_password": {
						Type:     types.StringType,
						Optional: true,
					},
				}),
			},
			"cluster_info": {
				Optional: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"state": {
						Type:     types.StringType,
						Optional: true,
					},
					"software_version": {
						Type:     types.StringType,
						Optional: true,
					},
					"created_time": {
						Type:     types.StringType,
						Optional: true,
					},
					"updated_time": {
						Type:     types.StringType,
						Optional: true,
					},
				}),
			},
			"cluster_version": {
				Type:     types.StringType,
				Optional: true,
			},
		},
	}, nil
}

func (r dataClusterNameType) NewDataSource(_ context.Context, p tfsdk.Provider) (tfsdk.DataSource, diag.Diagnostics) {
	return dataClusterName{
		p: *(p.(*provider)),
	}, nil
}

type dataClusterName struct {
	p provider
}

type inputClusterDetails struct {
	account_id   string
	cluster_name string
}

func (r dataClusterName) Read(ctx context.Context, req tfsdk.ReadDataSourceRequest, resp *tfsdk.ReadDataSourceResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider hasn't been configured before apply, likely because it depends on an unknown value from another resource.",
		)
		return
	}
	var attr inputClusterDetails

	attr1 := &attr
	req.Config.GetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("account_id"), &attr1.account_id)
	req.Config.GetAttribute(ctx, tftypes.NewAttributePath().WithAttributeName("cluster_name"), &attr1.cluster_name)
	apiClient := r.p.client
	projectId, getProjectOK, message := getProjectId(attr.account_id, apiClient)
	if !getProjectOK {
		resp.Diagnostics.AddError("Could not get project ID", message)
		return
	}

	res, r1, err := apiClient.ClusterApi.ListClusters(context.Background(), attr.account_id, projectId).Name(attr.cluster_name).Execute()

	if err != nil {
		b, _ := httputil.DumpResponse(r1, true)
		resp.Diagnostics.AddError("Could not extract the info of cluster info", string(b))
		return
	}

	list := res.GetData()
	Info := list[0].GetInfo()
	clusterId := Info.GetId()

	scheduleResp, r2, err1 := apiClient.BackupApi.ListBackupSchedules(ctx, attr.account_id, projectId).EntityId(clusterId).Execute()
	if err1 != nil {
		resp.Diagnostics.AddError("Could not fetch the backup schedule for the cluster "+r2.Status, "Try again")
		return
	}

	var cluster Cluster
	list1 := scheduleResp.GetData()
	scheduleId := list1[0].GetInfo().Id
	cluster, readOK, message := resourceClusterRead(attr.account_id, projectId, clusterId, scheduleId, make([]string, 0), true, make([]string, 0), true, apiClient)

	if !readOK {
		resp.Diagnostics.AddError("Could not read the state of the cluster", message)
		return
	}
	diags := resp.State.Set(ctx, &cluster)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}
