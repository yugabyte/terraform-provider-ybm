/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	//"github.com/hashicorp/terraform-plugin-log/tflog"
	//"fmt"
)

func (r *dataClusterName) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {

	resp.Schema = schema.Schema{
		Description: "The data source to fetch the cluster ID and other information about a cluster given the cluster name.",
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Description: "The ID of the account this cluster belongs to.",
				Computed:    true,
			},
			"project_id": schema.StringAttribute{
				Description: "The ID of the project this cluster belongs to.",
				Computed:    true,
			},
			"cluster_id": schema.StringAttribute{
				Description: "The ID of the cluster. Created automatically when a cluster is created. Used to get a specific cluster.",
				Computed:    true,
			},
			"cluster_name": schema.StringAttribute{
				Description: "The name of the cluster.",
				Required:    true,
			},
			"cluster_type": schema.StringAttribute{
				Description: "The type of the cluster.",
				Computed:    true,
			},
			"cloud_type": schema.StringAttribute{
				Description: "The cloud provider where the cluster is deployed: AWS or GCP. Default GCP.",
				Computed:    true,
			},
			"cluster_region_info": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"num_nodes": schema.Int64Attribute{
							Computed: true,
						},
						"region": schema.StringAttribute{
							Computed: true,
						},
						"num_zones": schema.Int64Attribute{
							Description: "Number of zones in the region.",
							Computed:    true,
						},
						"num_cores": schema.Int64Attribute{
							Computed: true,
						},
						"disk_size_gb": schema.Int64Attribute{
							Computed: true,
						},
						"disk_iops": schema.Int64Attribute{
							Computed: true,
						},
						"vpc_id": schema.StringAttribute{
							Computed: true,
						},
						"vpc_name": schema.StringAttribute{
							Computed: true,
						},
						"public_access": schema.BoolAttribute{
							Computed: true,
						},
						"is_preferred": schema.BoolAttribute{
							Computed: true,
						},
						"is_default": schema.BoolAttribute{
							Computed: true,
						},
						"backup_replication_gcp_target": schema.StringAttribute{
							Description: "GCS bucket name set as backup replication target.",
							Computed:    true,
						},
						"backup_region": schema.BoolAttribute{
							Description: "Indicates whether cluster backup data will be stored in this region.",
							Computed:    true,
						},
					},
				},
			},
			"backup_schedules": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{

						"state": schema.StringAttribute{

							Description: "The state of the backup schedule. Used to pause or resume the backup schedule. Valid values are ACTIVE or PAUSED.",
							Computed:    true,
						},

						"cron_expression": schema.StringAttribute{
							Description: "The cron expression for the backup schedule.",
							Computed:    true,
						},

						"time_interval_in_days": schema.Int64Attribute{
							Description: "The time interval in days for the backup schedule.",
							Computed:    true,
						},

						"retention_period_in_days": schema.Int64Attribute{
							Description: "The retention period of the backup schedule.",
							Computed:    true,
						},

						"backup_description": schema.StringAttribute{
							Description: "The description of the backup schedule.",
							Computed:    true,
						},

						"schedule_id": schema.StringAttribute{
							Description: "The ID of the backup schedule. Created automatically when the backup schedule is created. Used to get a specific backup schedule.",
							Computed:    true,
						},

						"incremental_interval_in_mins": schema.Int64Attribute{
							Description: "The time interval in minutes for the incremental backup schedule.",
							Computed:    true,
						},

						"use_roles": schema.BoolAttribute{
							Description: "Backup global YSQL roles in scheduled backups. Defaults to false.",
							Computed:    true,
						},
					},
				},
			},
			"cmk_spec": schema.SingleNestedAttribute{
				Description: "KMS Provider Configuration.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"provider_type": schema.StringAttribute{
						Description: "CMK Provider Type.",
						Computed:    true,
					},
					"is_enabled": schema.BoolAttribute{
						Description: "Is Enabled",
						Computed:    true,
					},
					"aws_cmk_spec": schema.SingleNestedAttribute{
						Description: "AWS CMK Provider Configuration.",
						Computed:    true,
						Attributes: map[string]schema.Attribute{
							"access_key": schema.StringAttribute{
								Description: "Access Key",
								Computed:    true,
							},
							"secret_key": schema.StringAttribute{
								Description: "Secret Key",
								Computed:    true,
							},
							"arn_list": schema.ListAttribute{
								Description: "AWS ARN List",
								ElementType: types.StringType,
								Computed:    true,
							},
						},
					},
					"gcp_cmk_spec": schema.SingleNestedAttribute{
						Description: "GCP CMK Provider Configuration.",
						Computed:    true,
						Attributes: map[string]schema.Attribute{
							"key_ring_name": schema.StringAttribute{
								Description: "Key Ring Name",
								Computed:    true,
							},
							"key_name": schema.StringAttribute{
								Description: "Key Name",
								Computed:    true,
							},
							"location": schema.StringAttribute{
								Description: "Location",
								Computed:    true,
							},
							"protection_level": schema.StringAttribute{
								Description: "Key Protection Level",
								Computed:    true,
							},
							"gcp_service_account": schema.SingleNestedAttribute{
								Description: "GCP Service Account",
								Computed:    true,
								Attributes: map[string]schema.Attribute{
									"type": schema.StringAttribute{
										Description: "Service Account Type",
										Computed:    true,
									},
									"project_id": schema.StringAttribute{
										Description: "GCP Project ID",
										Computed:    true,
									},
									"private_key": schema.StringAttribute{
										Description: "Private Key",
										Computed:    true,
									},
									"private_key_id": schema.StringAttribute{
										Description: "Private Key ID",
										Computed:    true,
									},
									"client_email": schema.StringAttribute{
										Description: "Client Email",
										Computed:    true,
									},
									"client_id": schema.StringAttribute{
										Description: "Client ID",
										Computed:    true,
									},
									"auth_uri": schema.StringAttribute{
										Description: "Auth URI",
										Computed:    true,
									},
									"token_uri": schema.StringAttribute{
										Description: "Token URI",
										Computed:    true,
									},
									"auth_provider_x509_cert_url": schema.StringAttribute{
										Description: "Auth Provider X509 Cert URL",
										Computed:    true,
									},
									"client_x509_cert_url": schema.StringAttribute{
										Description: "Client X509 Cert URL",
										Computed:    true,
									},
									"universe_domain": schema.StringAttribute{
										Description: "Google Universe Domain",
										Computed:    true,
									},
								},
							},
						},
					},
					"azure_cmk_spec": schema.SingleNestedAttribute{
						Description: "AZURE CMK Provider Configuration.",
						Computed:    true,
						Attributes: map[string]schema.Attribute{
							"client_id": schema.StringAttribute{
								Description: "Client ID",
								Computed:    true,
							},
							"client_secret": schema.StringAttribute{
								Description: "Client Secret",
								Computed:    true,
							},
							"tenant_id": schema.StringAttribute{
								Description: "Tenant ID",
								Computed:    true,
							},
							"key_vault_uri": schema.StringAttribute{
								Description: "Key Vault URI",
								Computed:    true,
							},
							"key_name": schema.StringAttribute{
								Description: "Key Name",
								Computed:    true,
							},
						},
					},
				},
			},
			"backup_replication_spec": schema.SingleNestedAttribute{
				Description: "Configuration for backup replication. Enables replication of cluster backups to offsite buckets.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"gcp_spec": schema.SingleNestedAttribute{
						Description: "GCP-specific backup replication configuration.",
						Computed:    true,
						Attributes: map[string]schema.Attribute{
							"enabled": schema.BoolAttribute{
								Description: "Whether GCP backup replication is enabled for this cluster.",
								Computed:    true,
							},
							"sync_cluster_spec": schema.SingleNestedAttribute{
								Description: "Backup replication configuration for SYNCHRONOUS clusters.",
								Computed:    true,
								Attributes: map[string]schema.Attribute{
									"replication_config": schema.SingleNestedAttribute{
										Description: "Replication configuration specifying the target GCS bucket and status information.",
										Computed:    true,
										Attributes: map[string]schema.Attribute{
											"target": schema.StringAttribute{
												Description: "The GCS bucket name where backups will be replicated",
												Computed:    true,
											},
											"assigned_region": schema.StringAttribute{
												Description: "The desginated backup region from where the backups will be replicated",
												Computed:    true,
											},
											"config_state": schema.StringAttribute{
												Description: "The current state of the replication configuration (e.g., ENABLED, DISABLED etc.).",
												Computed:    true,
											},
											"id": schema.StringAttribute{
												Description: "Unique identifier for the replication configuration.",
												Computed:    true,
											},
											"next_transfer_operation_time": schema.StringAttribute{
												Description: "Timestamp of the next scheduled backup transfer operation.",
												Computed:    true,
											},
											"latest_transfer_operation_details": schema.SingleNestedAttribute{
												Description: "Details about the most recent backup transfer operation.",
												Computed:    true,
												Attributes: map[string]schema.Attribute{
													"start_time": schema.StringAttribute{
														Description: "Start time of the latest transfer operation.",
														Computed:    true,
													},
													"end_time": schema.StringAttribute{
														Description: "End time of the latest transfer operation.",
														Computed:    true,
													},
													"status": schema.StringAttribute{
														Description: "Status of the latest transfer operation (e.g., SUCCESS, FAILED, IN_PROGRESS).",
														Computed:    true,
													},
												},
											},
											"expiry_on": schema.StringAttribute{
												Description: "Timestamp when this replication configuration expires, if applicable.",
												Computed:    true,
											},
										},
									},
									"configs_set_for_expiry": schema.ListNestedAttribute{
										Description: "List of replication configurations that are set to expire.",
										Computed:    true,
										NestedObject: schema.NestedAttributeObject{
											Attributes: map[string]schema.Attribute{
												"region": schema.StringAttribute{
													Description: "The region associated with this replication configuration.",
													Computed:    true,
												},
												"target": schema.StringAttribute{
													Description: "The GCS bucket name for this replication configuration",
													Computed:    true,
												},
												"config_state": schema.StringAttribute{
													Description: "The current state of the replication configuration (e.g., ENABLED, DISABLED etc.).",
													Computed:    true,
												},
												"id": schema.StringAttribute{
													Description: "Unique identifier for the replication configuration.",
													Computed:    true,
												},
												"next_transfer_operation_time": schema.StringAttribute{
													Description: "Timestamp of the next scheduled backup transfer operation.",
													Computed:    true,
												},
												"latest_transfer_operation_details": schema.SingleNestedAttribute{
													Description: "Details about the most recent backup transfer operation.",
													Computed:    true,
													Attributes: map[string]schema.Attribute{
														"start_time": schema.StringAttribute{
															Description: "Start time of the latest transfer operation.",
															Computed:    true,
														},
														"end_time": schema.StringAttribute{
															Description: "End time of the latest transfer operation.",
															Computed:    true,
														},
														"status": schema.StringAttribute{
															Description: "Status of the latest transfer operation (e.g., SUCCESS, FAILED, IN_PROGRESS).",
															Computed:    true,
														},
													},
												},
												"expiry_on": schema.StringAttribute{
													Description: "Timestamp when this replication configuration expires.",
													Computed:    true,
												},
											},
										},
									},
								},
							},
							"geo_partitioned_cluster_spec": schema.SingleNestedAttribute{
								Description: "Backup replication configuration for GEO_PARTITIONED clusters.",
								Computed:    true,
								Attributes: map[string]schema.Attribute{
									"replication_configs": schema.ListNestedAttribute{
										Description: "List of replication configurations, one for each region in the geo-partitioned cluster.",
										Computed:    true,
										NestedObject: schema.NestedAttributeObject{
											Attributes: map[string]schema.Attribute{
												"desired_region": schema.StringAttribute{
													Description: "The region name for this replication configuration. Must match one of the cluster's regions.",
													Computed:    true,
												},
												"target": schema.StringAttribute{
													Description: "The GCS bucket name where backups for this region will be replicated.",
													Computed:    true,
												},
												"config_state": schema.StringAttribute{
													Description: "The current state of the replication configuration (e.g., ENABLED, DISABLED etc.).",
													Computed:    true,
												},
												"id": schema.StringAttribute{
													Description: "Unique identifier for the replication configuration.",
													Computed:    true,
												},
												"next_transfer_operation_time": schema.StringAttribute{
													Description: "Timestamp of the next scheduled backup transfer operation.",
													Computed:    true,
												},
												"latest_transfer_operation_details": schema.SingleNestedAttribute{
													Description: "Details about the most recent backup transfer operation.",
													Computed:    true,
													Attributes: map[string]schema.Attribute{
														"start_time": schema.StringAttribute{
															Description: "Start time of the latest transfer operation.",
															Computed:    true,
														},
														"end_time": schema.StringAttribute{
															Description: "End time of the latest transfer operation.",
															Computed:    true,
														},
														"status": schema.StringAttribute{
															Description: "Status of the latest transfer operation (e.g., SUCCESS, FAILED, IN_PROGRESS).",
															Computed:    true,
														},
													},
												},
												"expiry_on": schema.StringAttribute{
													Description: "Timestamp when this replication configuration expires, if applicable.",
													Computed:    true,
												},
											},
										},
									},
									"configs_set_for_expiry": schema.ListNestedAttribute{
										Description: "List of replication configurations that are set to expire.",
										Computed:    true,
										NestedObject: schema.NestedAttributeObject{
											Attributes: map[string]schema.Attribute{
												"region": schema.StringAttribute{
													Description: "The region associated with this replication configuration.",
													Computed:    true,
												},
												"target": schema.StringAttribute{
													Description: "The GCS bucket name for this replication configuration.",
													Computed:    true,
												},
												"config_state": schema.StringAttribute{
													Description: "The current state of the replication configuration (e.g., ACTIVE, PENDING, ERROR).",
													Computed:    true,
												},
												"id": schema.StringAttribute{
													Description: "Unique identifier for the replication configuration.",
													Computed:    true,
												},
												"next_transfer_operation_time": schema.StringAttribute{
													Description: "Timestamp of the next scheduled backup transfer operation.",
													Computed:    true,
												},
												"latest_transfer_operation_details": schema.SingleNestedAttribute{
													Description: "Details about the most recent backup transfer operation.",
													Computed:    true,
													Attributes: map[string]schema.Attribute{
														"start_time": schema.StringAttribute{
															Description: "Start time of the latest transfer operation.",
															Computed:    true,
														},
														"end_time": schema.StringAttribute{
															Description: "End time of the latest transfer operation.",
															Computed:    true,
														},
														"status": schema.StringAttribute{
															Description: "Status of the latest transfer operation (e.g., SUCCESS, FAILED, IN_PROGRESS).",
															Computed:    true,
														},
													},
												},
												"expiry_on": schema.StringAttribute{
													Description: "Timestamp when this replication configuration expires.",
													Computed:    true,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			"cluster_tier": schema.StringAttribute{
				Description: "FREE (Sandbox) or PAID (Dedicated).",
				Computed:    true,
			},
			"fault_tolerance": schema.StringAttribute{
				Description: "The fault tolerance of the cluster.",
				Computed:    true,
			},
			"num_faults_to_tolerate": schema.Int64Attribute{
				Description: "The number of domain faults the cluster can tolerate.",
				Computed:    true,
			},
			"enterprise_security": schema.BoolAttribute{
				Description: "Whether the advanced security profile (enterprise security) is enabled on the cluster.",
				Computed:    true,
			},
			"cluster_allow_list_ids": schema.ListAttribute{
				Description: "List of IDs of the allow lists assigned to the cluster.",
				ElementType: types.StringType,
				Computed:    true,
			},
			"restore_backup_id": schema.StringAttribute{
				Description: "The ID of the backup to be restored to the cluster.",
				Computed:    true,
			},
			"node_config": schema.SingleNestedAttribute{
				Computed: true,
				Attributes: map[string]schema.Attribute{
					"num_cores": schema.Int64Attribute{
						Computed: true,
					},
					"disk_size_gb": schema.Int64Attribute{
						Computed: true,
					},
					"disk_iops": schema.Int64Attribute{
						Computed: true,
					},
				},
				DeprecationMessage: "Remove reliance on the attribute as it will be removed in the next major version of the provider. Please use cluster_region_info to read node config instead.",
			},
			"credentials": schema.SingleNestedAttribute{
				Computed: true,
				Attributes: map[string]schema.Attribute{
					"username": schema.StringAttribute{
						Computed: true,
					},
					"password": schema.StringAttribute{
						Computed: true,
					},
					"ysql_username": schema.StringAttribute{
						Computed: true,
					},
					"ysql_password": schema.StringAttribute{
						Computed: true,
					},
					"ycql_username": schema.StringAttribute{
						Computed: true,
					},
					"ycql_password": schema.StringAttribute{
						Computed: true,
					},
				},
			},
			"cluster_info": schema.SingleNestedAttribute{
				Computed: true,
				Attributes: map[string]schema.Attribute{
					"state": schema.StringAttribute{
						Computed: true,
					},
					"software_version": schema.StringAttribute{
						Computed: true,
					},
					"created_time": schema.StringAttribute{
						Computed: true,
					},
					"updated_time": schema.StringAttribute{
						Computed: true,
					},
				},
			},
			"cluster_version": schema.StringAttribute{
				Computed: true,
			},
			"database_track": schema.StringAttribute{
				Description: "The track of the database. Stable or Preview.",
				Computed:    true,
			},
			"desired_state": schema.StringAttribute{
				Description: "The desired state of the database, Active or Paused. This parameter can be used to pause/resume a cluster.",
				Computed:    true,
			},
			"desired_connection_pooling_state": schema.StringAttribute{
				Description: "The desired connection pooling state of the cluster, Enabled or Disabled.",
				Computed:    true,
			},
			"cluster_endpoints": schema.MapAttribute{
				Description: "The endpoints used to connect to the cluster by region.",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"endpoints": schema.ListNestedAttribute{
				Description: "The endpoints used to connect to the cluster.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"accessibility_type": schema.StringAttribute{
							Description: "The accessibility type of the endpoint. PUBLIC or PRIVATE.",
							Computed:    true,
						},
						"host": schema.StringAttribute{
							Description: "The host of the endpoint.",
							Computed:    true,
						},
						"region": schema.StringAttribute{
							Description: "The region of the endpoint.",
							Computed:    true,
						},
					},
				},
				Computed: true,
			},
			"cluster_certificate": schema.StringAttribute{
				Description: "The certificate used to connect to the cluster.",
				Computed:    true,
			},
		},
	}
}

func newDataClusterName() datasource.DataSource {
	return &dataClusterName{}
}

func (r *dataClusterName) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster"
}

func (r *dataClusterName) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type dataClusterName struct {
	p provider
}

type inputClusterDetails struct {
	account_id   string
	cluster_name string
}

func (r dataClusterName) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {

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

		ScheduleID: types.StringValue(scheduleId),
	}
	backUpSchedule = append(backUpSchedule, backUpInfo)
	cluster, readOK, message := resourceClusterRead(ctx, clusterId, backUpSchedule, make([]string, 0), true, make([]string, 0), true, "", apiClient)

	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the cluster", message)
		return
	}
	cluster.RestoreBackupID = types.StringValue("")

	diags := resp.State.Set(ctx, &cluster)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}
