/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/yugabyte/terraform-provider-ybm/managed/fflags"
	//"github.com/hashicorp/terraform-plugin-log/tflog"
	//"fmt"
)

type dataClusterNameType struct{}

func (r dataClusterNameType) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {

	return tfsdk.Schema{
		Description: "The data source to fetch the cluster ID and other information about a cluster given the cluster name.",
		Attributes: map[string]tfsdk.Attribute{
			"account_id": {
				Description: "The ID of the account this cluster belongs to.",
				Type:        types.StringType,
				Computed:    true,
			},
			"project_id": {
				Description: "The ID of the project this cluster belongs to.",
				Type:        types.StringType,
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
					"vpc_name": {
						Type:     types.StringType,
						Computed: true,
					},
					"public_access": {
						Type:     types.BoolType,
						Computed: true,
					},
				}),
			},
			"backup_schedules": {
				Computed:   true,
				Attributes: tfsdk.ListNestedAttributes(getBackupScheduleAttributes()),
			},
			"cmk_spec": {
				Description: "KMS Provider Configuration.",
				Computed:    true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"provider_type": {
						Description: "CMK Provider Type.",
						Type:        types.StringType,
						Computed:    true,
					},
					"is_enabled": {
						Description: "Is Enabled",
						Type:        types.BoolType,
						Computed:    true,
					},
					"aws_cmk_spec": {
						Description: "AWS CMK Provider Configuration.",
						Computed:    true,
						Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
							"access_key": {
								Description: "Access Key",
								Type:        types.StringType,
								Computed:    true,
							},
							"secret_key": {
								Description: "Secret Key",
								Type:        types.StringType,
								Computed:    true,
							},
							"arn_list": {
								Description: "AWS ARN List",
								Type:        types.ListType{ElemType: types.StringType},
								Computed:    true,
							},
						}),
					},
					"gcp_cmk_spec": {
						Description: "GCP CMK Provider Configuration.",
						Computed:    true,
						Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
							"key_ring_name": {
								Description: "Key Ring Name",
								Type:        types.StringType,
								Computed:    true,
							},
							"key_name": {
								Description: "Key Name",
								Type:        types.StringType,
								Computed:    true,
							},
							"location": {
								Description: "Location",
								Type:        types.StringType,
								Computed:    true,
							},
							"protection_level": {
								Description: "Key Protection Level",
								Type:        types.StringType,
								Computed:    true,
							},
							"gcp_service_account": {
								Description: "GCP Service Account",
								Computed:    true,
								Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
									"type": {
										Description: "Service Account Type",
										Type:        types.StringType,
										Computed:    true,
									},
									"project_id": {
										Description: "GCP Project ID",
										Type:        types.StringType,
										Computed:    true,
									},
									"private_key": {
										Description: "Private Key",
										Type:        types.StringType,
										Computed:    true,
									},
									"private_key_id": {
										Description: "Private Key ID",
										Type:        types.StringType,
										Computed:    true,
									},
									"client_email": {
										Description: "Client Email",
										Type:        types.StringType,
										Computed:    true,
									},
									"client_id": {
										Description: "Client ID",
										Type:        types.StringType,
										Computed:    true,
									},
									"auth_uri": {
										Description: "Auth URI",
										Type:        types.StringType,
										Computed:    true,
									},
									"token_uri": {
										Description: "Token URI",
										Type:        types.StringType,
										Computed:    true,
									},
									"auth_provider_x509_cert_url": {
										Description: "Auth Provider X509 Cert URL",
										Type:        types.StringType,
										Computed:    true,
									},
									"client_x509_cert_url": {
										Description: "Client X509 Cert URL",
										Type:        types.StringType,
										Computed:    true,
									},
									"universe_domain": {
										Description: "Google Universe Domain",
										Type:        types.StringType,
										Computed:    true,
									},
								}),
							},
						}),
					},
					"azure_cmk_spec": {
						Description: "AZURE CMK Provider Configuration.",
						Computed:    true,
						Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
							"client_id": {
								Description: "Client ID",
								Type:        types.StringType,
								Computed:    true,
							},
							"client_secret": {
								Description: "Client Secret",
								Type:        types.StringType,
								Computed:    true,
							},
							"tenant_id": {
								Description: "Tenant ID",
								Type:        types.StringType,
								Computed:    true,
							},
							"key_vault_uri": {
								Description: "Key Vault URI",
								Type:        types.StringType,
								Computed:    true,
							},
							"key_name": {
								Description: "Key Name",
								Type:        types.StringType,
								Computed:    true,
							},
						}),
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
			"num_faults_to_tolerate": {
				Description: "The number of domain faults the cluster can tolerate.",
				Type:        types.Int64Type,
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
					"disk_iops": {
						Type:     types.Int64Type,
						Computed: true,
					},
				}),
			},
			"credentials": {
				Computed: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"username": {
						Type:     types.StringType,
						Computed: true,
					},
					"password": {
						Type:     types.StringType,
						Computed: true,
					},
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
			"endpoints": {
				Description: "The endpoints used to connect to the cluster.",
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
					"accessibility_type": {
						Description: "The accessibility type of the endpoint. PUBLIC or PRIVATE.",
						Type:        types.StringType,
						Computed:    true,
					},
					"host": {
						Description: "The host of the endpoint.",
						Type:        types.StringType,
						Computed:    true,
					},
					"region": {
						Description: "The region of the endpoint.",
						Type:        types.StringType,
						Computed:    true,
					},
				}),
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
	req.Config.GetAttribute(ctx, path.Root("cluster_name"), &attr1.cluster_name)
	apiClient := r.p.client
	clusterName := attr.cluster_name

	accountId, getAccountOK, message = getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get the project ID", message)
		return
	}

	res, r1, err := apiClient.ClusterApi.ListClusters(context.Background(), accountId, projectId).Name(clusterName).Execute()

	if err != nil {
		errMsg := getErrorMessage(r1, err)
		resp.Diagnostics.AddError("Unable to extract the following cluster information: ", errMsg)
		return
	}

	clusterList := res.GetData()
	if len(clusterList) == 0 {
		resp.Diagnostics.AddError("Unable to extract the following cluster information: ", fmt.Sprintf("The cluster %v doesn't exist", clusterName))
		return
	}
	clusterInfo := clusterList[0].GetInfo()
	clusterId := clusterInfo.GetId()

	scheduleResp, r2, err1 := apiClient.BackupApi.ListBackupSchedules(ctx, accountId, projectId).EntityId(clusterId).Execute()
	if err1 != nil {
		resp.Diagnostics.AddError("Unable to fetch the backup schedule for the cluster "+r2.Status, "Try again.")
		return
	}

	var cluster Cluster
	backupScheduleList := scheduleResp.GetData()
	if len(backupScheduleList) == 0 {
		resp.Diagnostics.AddError("The default backup schedule was not found for the cluster ", clusterName)
		return
	}
	scheduleId := backupScheduleList[0].GetInfo().Id
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

func getBackupScheduleDsAttributes() map[string]tfsdk.Attribute {

	backupScheduleAttributes := map[string]tfsdk.Attribute{

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
	}

	if fflags.IsFeatureFlagEnabled(fflags.INCREMENTAL_BACKUP) {
		backupScheduleAttributes["incremental_interval_in_mins"] = tfsdk.Attribute{
			Description: "The time interval in minutes for the incremental backup schedule.",
			Type:        types.Int64Type,
			Computed:    true,
		}
	}

	return backupScheduleAttributes

}
