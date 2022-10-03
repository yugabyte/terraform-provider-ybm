/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	//"github.com/hashicorp/terraform-plugin-log/tflog"
	//"fmt"
)

type dataClusterNameType struct{}

func (r dataClusterNameType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {

	return tfsdk.Schema{
		Description: "The data source to fetch the cluster ID and other information about a cluster given the cluster name.",
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this cluster belongs to. To be provided if there are multiple accounts associated with the user.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this cluster belongs to.",
				Type:        types.StringType,
				Optional:    true,
				Computed:    true,
			},

			"cluster_id": {
				Description: "The ID of the cluster. Created automatically when a cluster is created. Used to get a specific cluster.",
				Type:        types.StringType,
				Computed:    true,
			},
			"cluster_name": {
				Description: "The name of the cluster.",
				Type:        types.StringType,
				Required:    true,
			},
			"cluster_type": {
				Description: "The type of the cluster.",
				Type:        types.StringType,
				Computed:    true,
			},
			"cloud_type": {
				Description: "The cloud provider where the cluster is deployed: AWS or GCP. Default GCP.",
				Type:        types.StringType,
				Computed:    true,
			},
			"cluster_region_info": {
				Computed: true,
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
					"num_nodes": {
						Type:     types.Int64Type,
						Computed: true,
					},
					"region": {
						Type:     types.StringType,
						Computed: true,
					},
					"vpc_id": {
						Type:     types.StringType,
						Computed: true,
					},
				}),
			},

			"backup_schedules": {
				Computed: true,
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{

					"state": {

						Description: "The state of the backup schedule. Used to pause or resume the backup schedule. Valid values are ACTIVE or PAUSED.",
						Type:        types.StringType,
						Computed:    true,
					},

					"cron_expression": {
						Description: "The cron expression for the backup schedule.",
						Type:        types.StringType,
						Computed:    true,
					},

					"time_interval_in_days": {
						Description: "The time interval in days for the backup schedule.",
						Type:        types.Int64Type,
						Computed:    true,
					},

					"retention_period_in_days": {
						Description: "The retention period of the backup schedule.",
						Type:        types.Int64Type,
						Computed:    true,
					},

					"backup_description": {
						Description: "The description of the backup schedule.",
						Type:        types.StringType,
						Computed:    true,
					},

					"schedule_id": {
						Description: "The ID of the backup schedule. Created automatically when the backup schedule is created. Used to get a specific backup schedule.",
						Type:        types.StringType,
						Computed:    true,
					},
				}),
			},

			"cluster_tier": {
				Description: "FREE (Sandbox) or PAID (Dedicated).",
				Type:        types.StringType,
				Computed:    true,
			},
			"fault_tolerance": {
				Description: "The fault tolerance of the cluster.",
				Type:        types.StringType,
				Computed:    true,
			},
			"cluster_allow_list_ids": {
				Description: "List of IDs of the allow lists assigned to the cluster.",
				Type: types.ListType{
					ElemType: types.StringType,
				},
				Computed: true,
			},
			"restore_backup_id": {
				Description: "The ID of the backup to be restored to the cluster.",
				Type:        types.StringType,
				Computed:    true,
			},
			"node_config": {
				Computed: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"num_cores": {
						Type:     types.Int64Type,
						Computed: true,
					},
					"disk_size_gb": {
						Type:     types.Int64Type,
						Computed: true,
					},
				}),
			},
			"credentials": {
				Computed: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"ysql_username": {
						Type:     types.StringType,
						Computed: true,
					},
					"ysql_password": {
						Type:     types.StringType,
						Computed: true,
					},
					"ycql_username": {
						Type:     types.StringType,
						Computed: true,
					},
					"ycql_password": {
						Type:     types.StringType,
						Computed: true,
					},
				}),
			},
			"cluster_info": {
				Computed: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"state": {
						Type:     types.StringType,
						Computed: true,
					},
					"software_version": {
						Type:     types.StringType,
						Computed: true,
					},
					"created_time": {
						Type:     types.StringType,
						Computed: true,
					},
					"updated_time": {
						Type:     types.StringType,
						Computed: true,
					},
				}),
			},
			"cluster_version": {
				Type:     types.StringType,
				Computed: true,
			},
			"database_track": {
				Description: "The track of the database. Stable or Preview.",
				Type:        types.StringType,
				Computed:    true,
			},
			"desired_state": {
				Description: "The desired state of the database, Active or Paused. This parameter can be used to pause/resume a cluster.",
				Type:        types.StringType,
				Computed:    true,
			},
			"cluster_endpoints": {
				Description: "The endpoints used to connect to the cluster by region.",
				Type: types.MapType{
					ElemType: types.StringType,
				},
				Optional: true,
				Computed: true,
			},
			"cluster_certificate": {
				Description: "The certificate used to connect to the cluster.",
				Type:        types.StringType,
				Computed:    true,
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
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}
	var attr inputClusterDetails
	var accountId, message string
	var getAccountOK bool

	attr1 := &attr
	req.Config.GetAttribute(ctx, path.Root("account_id"), &attr1.account_id)
	req.Config.GetAttribute(ctx, path.Root("cluster_name"), &attr1.cluster_name)
	apiClient := r.p.client
	if attr.account_id != "" {
		accountId = attr.account_id
	} else {
		accountId, getAccountOK, message = getAccountId(ctx, apiClient)
		if !getAccountOK {
			resp.Diagnostics.AddError("Unable to get account ID", message)
			return
		}
	}
	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get the project ID", message)
		return
	}

	res, r1, err := apiClient.ClusterApi.ListClusters(context.Background(), accountId, projectId).Name(attr.cluster_name).Execute()

	if err != nil {
		errMsg := getErrorMessage(r1, err)
		resp.Diagnostics.AddError("Unable to extract the following cluster information: ", errMsg)
		return
	}

	list := res.GetData()
	Info := list[0].GetInfo()
	clusterId := Info.GetId()

	scheduleResp, r2, err1 := apiClient.BackupApi.ListBackupSchedules(ctx, accountId, projectId).EntityId(clusterId).Execute()
	if err1 != nil {
		resp.Diagnostics.AddError("Unable to fetch the backup schedule for the cluster "+r2.Status, "Try again.")
		return
	}

	var cluster Cluster
	list1 := scheduleResp.GetData()
	scheduleId := list1[0].GetInfo().Id
	var backUpSchedule []BackupScheduleInfo
	backUpInfo := BackupScheduleInfo{

		ScheduleID: types.String{Value: scheduleId},
	}
	backUpSchedule = append(backUpSchedule, backUpInfo)
	cluster, readOK, message := resourceClusterRead(ctx, accountId, projectId, clusterId, backUpSchedule, make([]string, 0), true, make([]string, 0), true, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the cluster", message)
		return
	}
	diags := resp.State.Set(ctx, &cluster)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}
