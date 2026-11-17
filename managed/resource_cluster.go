/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/sethvargo/go-retry"
	"github.com/yugabyte/terraform-provider-ybm/managed/fflags"
	"github.com/yugabyte/terraform-provider-ybm/managed/util"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

// Short function for Base64 encoding
func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func shouldUseRestoreBackupID(backupID types.String) bool {
	return !backupID.IsUnknown() && !backupID.IsNull() && backupID.ValueString() != ""
}

func (r *resourceCluster) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"account_id": schema.StringAttribute{
			Description: "The ID of the account this cluster belongs to.",
			Computed:    true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseNonNullStateForUnknown(),
			},
		},
		"project_id": schema.StringAttribute{
			Description: "The ID of the project this cluster belongs to.",
			Computed:    true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseNonNullStateForUnknown(),
			},
		},
		"cluster_id": schema.StringAttribute{
			Description: "The ID of the cluster. Created automatically when a cluster is created. Used to get a specific cluster.",
			Computed:    true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseNonNullStateForUnknown(),
			},
		},
		"cluster_name": schema.StringAttribute{
			Description: "The name of the cluster.",
			Required:    true,
		},
		"cluster_type": schema.StringAttribute{
			Description: "The type of the cluster. SYNCHRONOUS or GEO_PARTITIONED",
			Required:    true,
			Validators: []validator.String{
				stringvalidator.OneOf("SYNCHRONOUS", "GEO_PARTITIONED"),
			},
		},
		"cloud_type": schema.StringAttribute{
			Description: "The cloud provider where the cluster is deployed: AWS, AZURE or GCP.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseNonNullStateForUnknown(),
			},
			Validators: []validator.String{stringvalidator.OneOf("AWS", "GCP", "AZURE")},
		},
		"cluster_region_info": schema.ListNestedAttribute{
			Required: true,
			NestedObject: schema.NestedAttributeObject{
				Attributes: map[string]schema.Attribute{
					"num_nodes": schema.Int64Attribute{
						Required: true,
					},
					"ignore_num_nodes_changes": schema.BoolAttribute{
						Description: "When true, Terraform ignores num_nodes changes made outside Terraform, such as changes made by an ACTIVE autoscaler policy. Explicit num_nodes changes in the Terraform configuration are still applied. Set this to true for regions managed by an ACTIVE autoscaler policy.",
						Optional:    true,
					},
					"region": schema.StringAttribute{
						Required: true,
					},
					"num_zones": schema.Int64Attribute{
						Description: "Number of zones in the region.",
						Optional:    true,
						Computed:    true,
					},
					"num_cores": schema.Int64Attribute{
						Description: "Number of CPU cores in the nodes of the region.",
						Optional:    true,
						Computed:    true,
						Validators: []validator.Int64{
							int64validator.ExactlyOneOf(
								path.MatchRelative().AtParent().AtParent().AtParent().AtName("node_config").AtName("num_cores"),
								path.MatchRelative(),
							),
						},
					},
					"disk_size_gb": schema.Int64Attribute{
						Description: "Disk size of the nodes of the region.",
						Optional:    true,
						Computed:    true,
						Validators: []validator.Int64{
							int64validator.ConflictsWith(
								path.MatchRelative().AtParent().AtParent().AtParent().AtName("node_config").AtName("disk_size_gb"),
							),
						},
					},
					"disk_iops": schema.Int64Attribute{
						Description: "Disk IOPS of the nodes of the region.",
						Optional:    true,
						Computed:    true,
						Validators: []validator.Int64{
							int64validator.ConflictsWith(
								path.MatchRelative().AtParent().AtParent().AtParent().AtName("node_config").AtName("disk_iops"),
							),
						},
					},
					"vpc_id": schema.StringAttribute{
						Optional: true,
						Computed: true,
						Validators: []validator.String{
							stringvalidator.ConflictsWith(path.MatchRelative().AtParent().AtName("vpc_name")),
						},
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseNonNullStateForUnknown(),
						},
					},
					"vpc_name": schema.StringAttribute{
						Optional: true,
						Computed: true,
						Validators: []validator.String{
							stringvalidator.ConflictsWith(path.MatchRelative().AtParent().AtName("vpc_id")),
						},
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseNonNullStateForUnknown(),
						},
					},
					"public_access": schema.BoolAttribute{
						Optional: true,
						Computed: true,
						PlanModifiers: []planmodifier.Bool{
							boolplanmodifier.UseNonNullStateForUnknown(),
						},
					},
					"is_preferred": schema.BoolAttribute{
						Optional: true,
						Computed: true,
						PlanModifiers: []planmodifier.Bool{
							boolplanmodifier.UseNonNullStateForUnknown(),
						},
					},
					"is_default": schema.BoolAttribute{
						Optional: true,
						Computed: true,
						PlanModifiers: []planmodifier.Bool{
							boolplanmodifier.UseNonNullStateForUnknown(),
						},
					},
					"backup_replication_gcp_target": schema.StringAttribute{
						Description: "GCS bucket name for backup replication target",
						Computed:    true,
					},
					"backup_region": schema.BoolAttribute{
						Description: "Indicates whether cluster backup data will be stored in this region.",
						Computed:    true,
					},
				},
			},
		},
		"backup_replication_spec": schema.SingleNestedAttribute{
			Description: "Configuration for backup replication. Enables replication of cluster backups to offsite buckets.",
			Optional:    true,
			Computed:    true,
			Attributes: map[string]schema.Attribute{
				"gcp_spec": schema.SingleNestedAttribute{
					Description: "GCP-specific backup replication configuration.",
					Optional:    true,
					Computed:    true,
					Validators: []validator.Object{
						objectvalidator.AlsoRequires(
							path.MatchRoot("cloud_type"),
						),
					},

					Attributes: map[string]schema.Attribute{
						"enabled": schema.BoolAttribute{
							Description: "Whether GCP backup replication is enabled for this cluster.",
							Optional:    true,
							Computed:    true,
							PlanModifiers: []planmodifier.Bool{
								boolplanmodifier.UseNonNullStateForUnknown(),
							},
						},
						"sync_cluster_spec": schema.SingleNestedAttribute{
							Description: "Backup replication configuration for SYNCHRONOUS clusters.",
							Optional:    true,
							Computed:    true,
							Validators: []validator.Object{
								objectvalidator.ConflictsWith(
									path.MatchRelative().AtParent().AtName("geo_partitioned_cluster_spec"),
								),
							},
							Attributes: map[string]schema.Attribute{
								"replication_config": schema.SingleNestedAttribute{
									Description: "Replication configuration specifying the target GCS bucket and status information.",
									Required:    true,
									Attributes: map[string]schema.Attribute{
										"target": schema.StringAttribute{
											Description: "The GCS bucket name where backups will be replicated",
											Required:    true,
											Validators: []validator.String{
												stringvalidator.RegexMatches(
													regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`),
													"GCS bucket names must contain only lowercase letters, numbers, hyphens, and underscores, and must start and end with a letter or number",
												),
											},
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
												Validators: []validator.String{
													stringvalidator.RegexMatches(
														regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`),
														"GCS bucket names must contain only lowercase letters, numbers, hyphens, and underscores, and must start and end with a letter or number",
													),
												},
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
							Optional:    true,
							Computed:    true,
							Validators: []validator.Object{
								objectvalidator.ConflictsWith(
									path.MatchRelative().AtParent().AtName("sync_cluster_spec"),
								),
							},
							Attributes: map[string]schema.Attribute{
								"replication_configs": schema.ListNestedAttribute{
									Description: "List of replication configurations, one for each region in the geo-partitioned cluster.",
									Optional:    true,
									Computed:    true,
									NestedObject: schema.NestedAttributeObject{
										Attributes: map[string]schema.Attribute{
											"desired_region": schema.StringAttribute{
												Description: "The region name for this replication configuration. Must match one of the cluster's regions.",
												Required:    true,
											},
											"target": schema.StringAttribute{
												Description: "The GCS bucket name where backups for this region will be replicated.",
												Required:    true,
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
		"backup_schedules": schema.ListNestedAttribute{
			Optional: true,
			NestedObject: schema.NestedAttributeObject{
				Attributes: map[string]schema.Attribute{
					"state": schema.StringAttribute{
						Description: "The state of the backup schedule. Used to pause or resume the backup schedule. Valid values are ACTIVE or PAUSED.",
						Computed:    true,
						Optional:    true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseNonNullStateForUnknown(),
						},
					},

					"cron_expression": schema.StringAttribute{
						Description: "The cron expression for the backup schedule",
						Computed:    true,
						Optional:    true,
					},

					"time_interval_in_days": schema.Int64Attribute{
						Description: "The time interval in days for the backup schedule.",
						Computed:    true,
						Optional:    true,
					},

					"retention_period_in_days": schema.Int64Attribute{
						Description: "The retention period of the backup schedule.",
						Computed:    true,
						Optional:    true,
						PlanModifiers: []planmodifier.Int64{
							int64planmodifier.UseNonNullStateForUnknown(),
						},
					},

					"backup_description": schema.StringAttribute{
						Description: "The description of the backup schedule.",
						Computed:    true,
						Optional:    true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseNonNullStateForUnknown(),
						},
					},

					"schedule_id": schema.StringAttribute{
						Description: "The ID of the backup schedule. Created automatically when the backup schedule is created. Used to get a specific backup schedule.",
						Computed:    true,
						Optional:    true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseNonNullStateForUnknown(),
						},
					},
					"incremental_interval_in_mins": schema.Int64Attribute{
						Description: "The time interval in minutes for the incremental backup schedule.",
						Optional:    true,
						Validators:  []validator.Int64{int64validator.AtLeast(60)},
					},
					"use_roles": schema.BoolAttribute{
						Description: "Backup global YSQL roles in scheduled backups. Defaults to false.",
						Optional:    true,
						Computed:    true,
					},
				},
			},
		},
		"cmk_spec": schema.SingleNestedAttribute{
			Description: "KMS Provider Configuration.",
			Optional:    true,
			Attributes: map[string]schema.Attribute{
				"provider_type": schema.StringAttribute{
					Description: "CMK Provider Type.",
					Required:    true,
					Validators:  []validator.String{stringvalidator.OneOf("AWS", "GCP", "AZURE")},
				},
				"is_enabled": schema.BoolAttribute{
					Description: "Is Enabled",
					Required:    true,
				},
				"aws_cmk_spec": schema.SingleNestedAttribute{
					Description: "AWS CMK Provider Configuration.",
					Optional:    true,
					Attributes: map[string]schema.Attribute{
						"access_key": schema.StringAttribute{
							Description: "Access Key",
							Required:    true,
						},
						"secret_key": schema.StringAttribute{
							Description: "Secret Key",
							Required:    true,
						},
						"arn_list": schema.ListAttribute{
							Description: "AWS ARN List",
							ElementType: types.StringType,
							Required:    true,
						},
					},
				},
				"gcp_cmk_spec": schema.SingleNestedAttribute{
					Description: "GCP CMK Provider Configuration.",
					Optional:    true,
					Attributes: map[string]schema.Attribute{
						"key_ring_name": schema.StringAttribute{
							Description: "Key Ring Name",
							Required:    true,
						},
						"key_name": schema.StringAttribute{
							Description: "Key Name",
							Required:    true,
						},
						"location": schema.StringAttribute{
							Description: "Location",
							Required:    true,
						},
						"protection_level": schema.StringAttribute{
							Description: "Key Protection Level",
							Required:    true,
						},
						"gcp_service_account": schema.SingleNestedAttribute{
							Description: "GCP Service Account",
							Required:    true,
							Attributes: map[string]schema.Attribute{
								"type": schema.StringAttribute{
									Description: "Service Account Type",
									Required:    true,
								},
								"project_id": schema.StringAttribute{
									Description: "GCP Project ID",
									Required:    true,
								},
								"private_key": schema.StringAttribute{
									Description: "Private Key",
									Required:    true,
								},
								"private_key_id": schema.StringAttribute{
									Description: "Private Key ID",
									Required:    true,
								},
								"client_email": schema.StringAttribute{
									Description: "Client Email",
									Required:    true,
								},
								"client_id": schema.StringAttribute{
									Description: "Client ID",
									Required:    true,
								},
								"auth_uri": schema.StringAttribute{
									Description: "Auth URI",
									Required:    true,
								},
								"token_uri": schema.StringAttribute{
									Description: "Token URI",
									Required:    true,
								},
								"auth_provider_x509_cert_url": schema.StringAttribute{
									Description: "Auth Provider X509 Cert URL",
									Required:    true,
								},
								"client_x509_cert_url": schema.StringAttribute{
									Description: "Client X509 Cert URL",
									Required:    true,
								},
								"universe_domain": schema.StringAttribute{
									Description: "Google Universe Domain",
									Optional:    true,
								},
							},
						},
					},
				},
				"azure_cmk_spec": schema.SingleNestedAttribute{
					Description: "AZURE CMK Provider Configuration.",
					Optional:    true,
					Attributes: map[string]schema.Attribute{
						"client_id": schema.StringAttribute{
							Description: "Azure Active Directory (AD) Client ID for Key Vault service principal.",
							Required:    true,
						},
						"client_secret": schema.StringAttribute{
							Description: "Azure AD Client Secret for Key Vault service principal.",
							Required:    true,
						},
						"tenant_id": schema.StringAttribute{
							Description: "Azure AD Tenant ID for Key Vault service principal.",
							Required:    true,
						},
						"key_vault_uri": schema.StringAttribute{
							Description: "URI of Azure Key Vault storing cryptographic keys.",
							Required:    true,
						},
						"key_name": schema.StringAttribute{
							Description: "Name of cryptographic key in Azure Key Vault.",
							Required:    true,
						},
					},
				},
			},
		},
		"cluster_tier": schema.StringAttribute{
			Description: "FREE (Sandbox) or PAID (Dedicated).",
			Required:    true,
			Validators:  []validator.String{stringvalidator.OneOf("FREE", "PAID")},
		},
		"fault_tolerance": schema.StringAttribute{
			Description: "The fault tolerance of the cluster. NONE, NODE, ZONE or REGION.",
			Optional:    true,
			Computed:    true,
			Validators:  []validator.String{stringvalidator.OneOf("NONE", "NODE", "ZONE", "REGION")},
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseNonNullStateForUnknown(),
			},
		},
		"num_faults_to_tolerate": schema.Int64Attribute{
			Description: "The number of domain faults the cluster can tolerate. 0 for NONE, 1 for ZONE and [1-3] for NODE and REGION",
			Optional:    true,
			Computed:    true,
			Validators:  []validator.Int64{int64validator.OneOf(0, 1, 2, 3)},
			PlanModifiers: []planmodifier.Int64{
				int64planmodifier.UseNonNullStateForUnknown(),
			},
		},
		"is_multi_cloud": schema.BoolAttribute{
			Description: "Set to true to deploy a cluster that spans multiple cloud providers. Optional; defaults to false. This value cannot be modified after the cluster is created.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.Bool{
				boolplanmodifier.UseNonNullStateForUnknown(),
			},
		},
		"enterprise_security": schema.BoolAttribute{
			Description: "Set to true to enable the advanced security profile (enterprise security) on the cluster. Optional; defaults to false. This value cannot be modified after the cluster is created.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.Bool{
				boolplanmodifier.UseNonNullStateForUnknown(),
			},
		},
		"cluster_allow_list_ids": schema.ListAttribute{
			Description: "List of IDs of the allow lists assigned to the cluster.",
			ElementType: types.StringType,
			Optional:    true,
		},
		"restore_backup_id": schema.StringAttribute{
			Description:        "The ID of the backup to be restored to the cluster.",
			DeprecationMessage: "Use the ybm_backup_restore resource instead. This attribute will be removed in a future release.",
			Optional:           true,
		},
		"node_config": schema.SingleNestedAttribute{
			Optional: true,
			Computed: true,
			Attributes: map[string]schema.Attribute{
				"num_cores": schema.Int64Attribute{
					Description: "Number of CPU cores in the node.",
					Optional:    true,
					Computed:    true,
				},
				"disk_size_gb": schema.Int64Attribute{
					Description: "Disk size of the node.",
					Computed:    true,
					Optional:    true,
					Validators: []validator.Int64{
						int64validator.ConflictsWith(
							path.MatchRelative().AtParent().AtParent().AtName("cluster_region_info").AtAnyListIndex().AtName("disk_size_gb"),
						),
					},
				},
				"disk_iops": schema.Int64Attribute{
					Description: "Disk IOPS of the node.",
					Computed:    true,
					Optional:    true,
					Validators: []validator.Int64{
						int64validator.ConflictsWith(
							path.MatchRelative().AtParent().AtParent().AtName("cluster_region_info").AtAnyListIndex().AtName("disk_iops"),
						),
					},
				},
			},
			DeprecationMessage: "Remove this attribute's configuration as it's no longer in use and the attribute will be removed in the next major version of the provider. Please use cluster_region_info to specify node config instead.",
		},
		"credentials": schema.SingleNestedAttribute{
			Description: `Credentials to be used by the database. Required only at the time of creation. Please provide 'username' and 'password' 
(which would be used in common for both YSQL and YCQL) OR all of 'ysql_username',
'ysql_password', 'ycql_username' and 'ycql_password' but not a mix of both.`,
			Optional: true,
			Attributes: map[string]schema.Attribute{
				"username": schema.StringAttribute{
					Description: "The username to be used for both YSQL and YCQL.",
					Optional:    true,
				},
				"password": schema.StringAttribute{
					Description: "The password to be used for both YSQL and YCQL. Note that this will be stored in the state file.",
					Optional:    true,
					Sensitive:   true,
				},
				"ysql_username": schema.StringAttribute{
					Description: "YSQL username for the database.",
					Optional:    true,
				},
				"ysql_password": schema.StringAttribute{
					Description: "YSQL password for the database. Note that this will be stored in the state file.",
					Optional:    true,
					Sensitive:   true,
				},
				"ycql_username": schema.StringAttribute{
					Description: "YCQL username for the database.",
					Optional:    true,
				},
				"ycql_password": schema.StringAttribute{
					Description: "YCQL password for the database. Note that this will be stored in the state file.",
					Optional:    true,
					Sensitive:   true,
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
					PlanModifiers: []planmodifier.String{
						stringplanmodifier.UseNonNullStateForUnknown(),
					},
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
			Description: "Database software release track for the cluster. Supported values are Extended and Rapid. Deprecated values are still accepted for backward compatibility: Production and Innovation map to Extended; Preview and Early Access map to Rapid.",
			MarkdownDescription: "Database software release track for the cluster. Supported values are `Extended` and `Rapid`.\n\n" +
				"> **Note:** Deprecated track names are still accepted for backward compatibility, but should be updated in configuration.\n" +
				"> - `Production` and `Innovation` are deprecated in favor of `Extended`.\n" +
				"> - `Preview` and `Early Access` are deprecated in favor of `Rapid`.",
			Optional: true,
			Computed: true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseNonNullStateForUnknown(),
			},
		},
		"desired_state": schema.StringAttribute{
			Description: "The desired state of the cluster, Active or Paused. This parameter can be used to pause/resume a cluster.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseNonNullStateForUnknown(),
			},
			Validators: []validator.String{
				// Validate string value must be "Active" or "Paused"
				stringvalidator.OneOf([]string{"Active", "Paused"}...),
			},
		},
		"desired_connection_pooling_state": schema.StringAttribute{
			Description: "The desired connection pooling state of the cluster, Enabled or Disabled. Can be used during or after cluster creation.",
			Optional:    true,
			Computed:    true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseNonNullStateForUnknown(),
			},
			Validators: []validator.String{
				// Validate string value must be "Enabled" or "Disabled"
				stringvalidator.OneOf([]string{"Enabled", "Disabled"}...),
			},
		},
		"cluster_endpoints": schema.MapAttribute{
			Description:        "The endpoints used to connect to the cluster.",
			DeprecationMessage: "This attribute is deprecated. Please use the 'endpoints' attribute instead.",
			ElementType:        types.StringType,
			Computed:           true,
		},
		"endpoints": schema.ListNestedAttribute{
			Description: "The endpoints used to connect to the cluster.",
			NestedObject: schema.NestedAttributeObject{
				Attributes: map[string]schema.Attribute{
					"accessibility_type": schema.StringAttribute{
						Description: "The accessibility type of the endpoint. PUBLIC or PRIVATE.",
						Computed:    true,
						Optional:    true,
					},
					"host": schema.StringAttribute{
						Description: "The host of the endpoint.",
						Computed:    true,
						Optional:    true,
					},
					"region": schema.StringAttribute{
						Description: "The region of the endpoint.",
						Computed:    true,
						Optional:    true,
					},
				},
			},
			Computed: true,
		},
		"cluster_certificate": schema.StringAttribute{
			Description: "The certificate used to connect to the cluster.",
			Computed:    true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseNonNullStateForUnknown(),
			},
		},
	}

	resp.Schema = schema.Schema{
		Description: `The resource to create a YugabyteDB cluster. Use this resource to create both 
single- and multi-region clusters. You can also use this resource to bind allow lists to the cluster 
being created; restore previously taken backups to the cluster being created; 
and modify the backup schedule of the cluster being created.`,
		Attributes: attributes,
	}
}

func newResourceCluster() resource.Resource {
	return &resourceCluster{}
}

func (r *resourceCluster) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster"
}

func (r *resourceCluster) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type resourceCluster struct {
	p provider
}

var _ resource.ResourceWithValidateConfig = &resourceCluster{}
var _ resource.ResourceWithModifyPlan = &resourceCluster{}

// ModifyPlan blocks changing ignore_num_nodes_changes while the current YBM node
// count differs from configured num_nodes. Node counts must be aligned before
// changing Terraform's node-count management behavior.
func (r resourceCluster) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	resp.Plan = req.Plan

	// Destroy plans have a null planned state.
	if req.Plan.Raw.IsNull() {
		return
	}

	modifySingleRegionDefaultPlan(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}

	// Create has no prior state to compare.
	if req.State.Raw.IsNull() {
		return
	}

	// Only decode the attributes we need. Full Cluster Get() fails on null nested
	// attributes (credentials, cmk_spec, backup_replication_spec, etc.).
	var configRegions []RegionInfo
	var stateRegions []RegionInfo
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("cluster_region_info"), &configRegions)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("cluster_region_info"), &stateRegions)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stateByRegion := regionInfoByRegion(stateRegions)

	needsLiveNumNodes := false
	for _, configRegion := range configRegions {
		stateRegion, ok := stateByRegion[configRegion.Region.ValueString()]
		if !ok {
			continue
		}
		if ignoreNumNodesChangesEnabled(stateRegion) && !ignoreNumNodesChangesEnabled(configRegion) {
			needsLiveNumNodes = true
			break
		}
	}

	liveNumNodesByRegion := map[string]int64{}
	if needsLiveNumNodes {
		// Read masks node-count drift while ignore is enabled, so fetch the live
		// count when disabling ignore.
		var accountId, projectId, clusterId types.String
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("account_id"), &accountId)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("project_id"), &projectId)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("cluster_id"), &clusterId)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if accountId.IsNull() || accountId.IsUnknown() ||
			projectId.IsNull() || projectId.IsUnknown() ||
			clusterId.IsNull() || clusterId.IsUnknown() ||
			r.p.client == nil {
			resp.Diagnostics.AddError(
				disableIgnoreNumNodesWithDriftErrorSummary,
				"Unable to verify the current YugabyteDB Aeon node count while disabling ignore_num_nodes_changes. "+
					"Ensure the cluster exists in state and the provider is configured, then retry.",
			)
			return
		}

		clusterResp, response, err := r.p.client.ClusterApi.GetCluster(ctx, accountId.ValueString(), projectId.ValueString(), clusterId.ValueString()).Execute()
		if err != nil {
			resp.Diagnostics.AddError(
				disableIgnoreNumNodesWithDriftErrorSummary,
				"Unable to fetch the current cluster node count while disabling ignore_num_nodes_changes: "+getErrorMessage(response, err),
			)
			return
		}
		for _, regionInfo := range clusterResp.Data.Spec.ClusterRegionInfo {
			region := regionInfo.PlacementInfo.CloudInfo.GetRegion()
			liveNumNodesByRegion[region] = int64(regionInfo.PlacementInfo.GetNumNodes())
		}
	}

	addIgnoreNumNodesDriftErrors(&resp.Diagnostics, configRegions, stateRegions, liveNumNodesByRegion)
}

// modifySingleRegionDefaultPlan keeps the plan in step with createClusterSpec, which forces
// is_default on the only region of a single-region cluster because YugabyteDB Aeon requires such
// a cluster to have a default region. Without this the provider reads back true where the plan
// said false - as happens when a multi-region cluster is shrunk to one region and the prior
// state carried false - and the apply fails with "Provider produced inconsistent result after
// apply".
func modifySingleRegionDefaultPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// With dynamically typed vars (e.g. list(any)), cluster_region_info can be wholly unknown
	// here; decoding it into []RegionInfo then fails with "unhandled unknown value". A list that
	// is unknown or null says nothing about the region count, so there is nothing to fix up.
	var planList types.List
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("cluster_region_info"), &planList)...)
	if resp.Diagnostics.HasError() || planList.IsNull() || planList.IsUnknown() {
		return
	}

	var configList types.List
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("cluster_region_info"), &configList)...)
	if resp.Diagnostics.HasError() || configList.IsNull() || configList.IsUnknown() {
		return
	}

	var planRegions []RegionInfo
	resp.Diagnostics.Append(planList.ElementsAs(ctx, &planRegions, false)...)
	if resp.Diagnostics.HasError() || len(planRegions) != 1 {
		return
	}

	var configRegions []RegionInfo
	resp.Diagnostics.Append(configList.ElementsAs(ctx, &configRegions, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// An explicit is_default = false can never hold for a single-region cluster, so report it at
	// plan time instead of letting the apply run and then fail.
	if len(configRegions) == 1 {
		configDefault := configRegions[0].IsDefault
		if !configDefault.IsNull() && !configDefault.IsUnknown() && !configDefault.ValueBool() {
			resp.Diagnostics.AddError(
				"Invalid is_default for a single region cluster",
				"The only region of a single region cluster is always its default region. Set is_default = true "+
					"for region "+configRegions[0].Region.ValueString()+", or leave it unset and the provider will.",
			)
			return
		}
	}

	// Unknown already resolves to whatever the apply returns; true is what the apply returns.
	if planRegions[0].IsDefault.IsUnknown() || planRegions[0].IsDefault.ValueBool() {
		return
	}

	resp.Diagnostics.Append(resp.Plan.SetAttribute(
		ctx,
		path.Root("cluster_region_info").AtListIndex(0).AtName("is_default"),
		types.BoolUnknown(),
	)...)
}

func EditBackupSchedule(ctx context.Context, backupScheduleStruct BackupScheduleInfo, scheduleId string, backupDes string, accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) error {
	return editBackupScheduleV2(ctx, backupScheduleStruct, scheduleId, backupDes, accountId, projectId, clusterId, apiClient)
}

func editBackupScheduleV2(ctx context.Context, backupScheduleStruct BackupScheduleInfo, scheduleId string, backupDes string, accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) error {
	if backupScheduleStruct.State.ValueString() != "" && backupScheduleStruct.RetentionPeriodInDays.ValueInt64() != 0 {
		backupRetentionPeriodInDays := int32(backupScheduleStruct.RetentionPeriodInDays.ValueInt64())
		backupScheduleSpec := *openapiclient.NewScheduleSpecV2WithDefaults()
		backupScheduleSpec.SetDescription(backupDes)
		backupScheduleSpec.SetRetentionPeriodInDays(backupRetentionPeriodInDays)
		backupScheduleSpec.SetState(openapiclient.ScheduleStateEnum(backupScheduleStruct.State.ValueString()))
		if !backupScheduleStruct.IncrementalIntervalInMins.IsNull() && !backupScheduleStruct.IncrementalIntervalInMins.IsUnknown() && backupScheduleStruct.IncrementalIntervalInMins.ValueInt64() != 0 {
			incrementalIntervalInMins := int32(backupScheduleStruct.IncrementalIntervalInMins.ValueInt64())
			backupScheduleSpec.SetIncrementalIntervalInMinutes(incrementalIntervalInMins)
		} else {
			backupScheduleSpec.UnsetIncrementalIntervalInMinutes()
		}
		if !backupScheduleStruct.TimeIntervalInDays.IsNull() && !backupScheduleStruct.TimeIntervalInDays.IsUnknown() && backupScheduleStruct.TimeIntervalInDays.ValueInt64() != 0 {
			timeIntervalInDays := int32(backupScheduleStruct.TimeIntervalInDays.ValueInt64())
			backupScheduleSpec.SetTimeIntervalInDays(timeIntervalInDays)
			backupScheduleSpec.UnsetCronExpression()
		}
		if backupScheduleStruct.CronExpression.ValueString() != "" {
			cronExp := backupScheduleStruct.CronExpression.ValueString()
			backupScheduleSpec.SetCronExpression(cronExp)
			backupScheduleSpec.UnsetTimeIntervalInDays()
		}
		if backupScheduleStruct.TimeIntervalInDays.ValueInt64() != 0 && backupScheduleStruct.CronExpression.ValueString() != "" {
			return errors.New("unable to create custom backup schedule. You can't pass both the cron expression and time interval in days")
		}

		if !backupScheduleStruct.UseRoles.IsNull() && !backupScheduleStruct.UseRoles.IsUnknown() {
			backupScheduleSpec.SetUseRoles(backupScheduleStruct.UseRoles.ValueBool())
		}

		_, res, err := apiClient.BackupApi.ModifyBackupScheduleV2(ctx, accountId, projectId, clusterId, scheduleId).ScheduleSpecV2(backupScheduleSpec).Execute()
		if err != nil {
			errMsg := getErrorMessage(res, err)
			return errors.New("Unable to modify the backup schedule. " + errMsg)
		}
	}
	return nil
}

func createClusterSpec(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, plan Cluster, state Cluster, clusterExists bool) (clusterSpec *openapiclient.ClusterSpec, clusterSpecOK bool, errorMessage string) {
	var diskSizeGb int32
	var diskSizeOK bool
	var memoryMb int32
	var memoryOK bool
	var trackId string
	var trackName string
	var trackIdOK bool
	var message string

	// Compute track ID for database version
	softwareInfo := *openapiclient.NewSoftwareInfoWithDefaults()
	if !plan.DatabaseTrack.IsUnknown() {
		trackName = plan.DatabaseTrack.ValueString()
		trackId, trackIdOK, message = getTrackId(ctx, apiClient, accountId, trackName)
		if !trackIdOK {
			return nil, false, message
		}
		softwareInfo.SetTrackId(trackId)
	}

	var clusterRegionInfo []openapiclient.ClusterRegionInfo
	totalNodes := 0
	clusterType := plan.ClusterType.ValueString()
	isDefaultSet := false
	pseInfoMap := make(map[string]openapiclient.PrivateServiceEndpointRegionSpec)
	actualNumNodesMap := make(map[string]int32)
	stateRegionsByCode := regionInfoByRegion(state.ClusterRegionInfo)
	if clusterExists {
		clusterResp, response, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, state.ClusterID.ValueString()).Execute()
		if err != nil {
			errMsg := getErrorMessage(response, err)
			return nil, false, errMsg
		}
		for _, regionInfo := range clusterResp.Data.Spec.ClusterRegionInfo {
			region := regionInfo.PlacementInfo.CloudInfo.GetRegion()
			actualNumNodesMap[region] = regionInfo.PlacementInfo.GetNumNodes()
			if slices.Contains(regionInfo.GetAccessibilityTypes(), openapiclient.ACCESSIBILITYTYPE_PRIVATE_SERVICE_ENDPOINT) {
				pseInfoMap[region] = regionInfo.GetPrivateServiceEndpointInfo()
			}

		}
	}
	tflog.Debug(ctx, fmt.Sprintf("PSE info map is %v", pseInfoMap))

	for _, regionInfo := range plan.ClusterRegionInfo {
		regionNodes := int32(regionInfo.NumNodes.ValueInt64())
		if clusterExists {
			stateRegion, hasStateRegion := stateRegionsByCode[regionInfo.Region.ValueString()]
			actualNumNodes, hasActualNumNodes := actualNumNodesMap[regionInfo.Region.ValueString()]
			if hasStateRegion && hasActualNumNodes {
				regionNodes = resolveNumNodesForEdit(regionInfo, stateRegion, actualNumNodes)
			}
		}
		totalNodes += int(regionNodes)
		info := *openapiclient.NewClusterRegionInfo(
			*openapiclient.NewPlacementInfo(
				*openapiclient.NewCloudInfo(
					openapiclient.CloudEnum(plan.CloudType.ValueString()),
					regionInfo.Region.ValueString()), regionNodes),
		)
		if vpcName := regionInfo.VPCName.ValueString(); vpcName != "" {
			vpcData, err := getVPCByName(context.Background(), accountId, projectId, vpcName, apiClient)
			if err != nil {
				return nil, false, err.Error()
			}

			regionInfo.VPCID = types.StringValue(vpcData.Info.Id)
		}

		cloud := plan.CloudType.ValueString()
		tier := plan.ClusterTier.ValueString()
		region := regionInfo.Region.ValueString()
		var numCores int32
		// If regionInfo numCores is null, then it must be coming from the root level node info.
		if regionInfo.NumCores.IsNull() || regionInfo.NumCores.IsUnknown() {
			numCores = int32(plan.NodeConfig.NumCores.ValueInt64())
		} else {
			numCores = int32(regionInfo.NumCores.ValueInt64())
		}

		memoryMb, memoryOK, message = getMemoryFromInstanceType(ctx, apiClient, accountId, cloud, tier, region, numCores)
		if !memoryOK {
			return nil, false, message
		}

		if !regionInfo.DiskSizeGb.IsUnknown() && !regionInfo.DiskSizeGb.IsNull() {
			diskSizeGb = int32(regionInfo.DiskSizeGb.ValueInt64())
		} else if plan.NodeConfig != nil && !plan.NodeConfig.DiskSizeGb.IsUnknown() && !plan.NodeConfig.DiskSizeGb.IsNull() {
			diskSizeGb = int32(plan.NodeConfig.DiskSizeGb.ValueInt64())
		} else {
			diskSizeGb, diskSizeOK, message = getDiskSizeFromInstanceType(ctx, apiClient, accountId, cloud, tier, region, numCores)
			if !diskSizeOK {
				return nil, false, message
			}
		}

		nodeInfo := *openapiclient.NewOptionalClusterNodeInfo(numCores, memoryMb, diskSizeGb)
		if !regionInfo.DiskIops.IsUnknown() && !regionInfo.DiskIops.IsNull() && int32(regionInfo.DiskIops.ValueInt64()) > 0 {
			nodeInfo.SetDiskIops(int32(regionInfo.DiskIops.ValueInt64()))
		} else if plan.NodeConfig != nil && !plan.NodeConfig.DiskIops.IsNull() && !plan.NodeConfig.DiskIops.IsNull() && int32(plan.NodeConfig.DiskIops.ValueInt64()) > 0 {
			nodeInfo.SetDiskIops(int32(plan.NodeConfig.DiskIops.ValueInt64()))
		} else {
			nodeInfo.UnsetDiskIops()
		}

		info.SetNodeInfo(nodeInfo)

		// Create an array of AccessibilityType and populate it according to
		// the following logic:
		// if the cluster is in a private VPC, it MUST always have PRIVATE.
		// if the cluster is NOT in a private VPC, it MUST always have PUBLIC.
		// if the cluster is in a private VPC and customer wants public access, it MUST have PRIVATE and PUBLIC.
		var accessibilityTypes []openapiclient.AccessibilityType

		if vpcID := regionInfo.VPCID.ValueString(); vpcID != "" {
			info.PlacementInfo.SetVpcId(vpcID)
			accessibilityTypes = append(accessibilityTypes, openapiclient.ACCESSIBILITYTYPE_PRIVATE)

			if regionInfo.PublicAccess.ValueBool() {
				accessibilityTypes = append(accessibilityTypes, openapiclient.ACCESSIBILITYTYPE_PUBLIC)
			}
		} else {
			accessibilityTypes = append(accessibilityTypes, openapiclient.ACCESSIBILITYTYPE_PUBLIC)

			// If the value is specified, and it is false, then it is an error because the user
			// wants disabled public access on a non-dedicated VPC cluster.
			if !regionInfo.PublicAccess.IsUnknown() && !regionInfo.PublicAccess.ValueBool() {
				tflog.Debug(ctx, fmt.Sprintf("Cluster %v is in a public VPC and public access is disabled. ", plan.ClusterName.ValueString()))
				return nil, false, "Cluster is in a public VPC and public access is disabled. Please enable public access."
			}
		}

		if clusterExists {
			if pseEndpointSpec, exists := pseInfoMap[regionInfo.Region.ValueString()]; exists {
				accessibilityTypes = append(accessibilityTypes, openapiclient.ACCESSIBILITYTYPE_PRIVATE_SERVICE_ENDPOINT)
				info.SetPrivateServiceEndpointInfo(pseEndpointSpec)
			}
		}

		// Set the accessibility type for the region
		info.SetAccessibilityTypes(accessibilityTypes)

		if clusterType == "SYNCHRONOUS" {
			info.PlacementInfo.SetMultiZone(false)
		}

		if fflags.IsFeatureFlagEnabled(fflags.MultiZoneSupport) {
			if !regionInfo.NumZones.IsNull() && !regionInfo.NumZones.IsUnknown() {
				info.PlacementInfo.SetNumZones(int32(regionInfo.NumZones.ValueInt64()))
			}
		}
		info.SetIsDefault(false)
		info.SetIsAffinitized(false)
		if !regionInfo.IsPreferred.IsUnknown() && !regionInfo.IsPreferred.IsNull() {
			info.SetIsAffinitized(regionInfo.IsPreferred.ValueBool())
		}

		if !regionInfo.IsDefault.IsUnknown() && !regionInfo.IsDefault.IsNull() && regionInfo.IsDefault.ValueBool() {
			if isDefaultSet {
				return nil, false, "Cluster must have exactly one default region."
			}
			info.SetIsDefault(regionInfo.IsDefault.ValueBool())
			isDefaultSet = true
		}

		clusterRegionInfo = append(clusterRegionInfo, info)
	}

	if clusterExists {
		if err := setBackupReplicationTargetsInEditClusterSpec(plan, clusterRegionInfo); err != nil {
			return nil, false, err.Error()
		}
	}

	// This is to pass in the region information to fetch memory and disk size
	regionCount := len(clusterRegionInfo)
	if regionCount == 1 {
		clusterRegionInfo[0].SetIsDefault(true)
	}

	// This is to support a redundant value in the API.
	// Needs to be removed once API cleans it up.
	isProduction := true
	if plan.ClusterTier.ValueString() == "FREE" {
		isProduction = false
	}

	clusterInfo := *openapiclient.NewClusterInfo(
		openapiclient.ClusterTier(plan.ClusterTier.ValueString()),
		int32(totalNodes),
		openapiclient.ClusterFaultTolerance(plan.FaultTolerance.ValueString()),
		isProduction,
	)

	clusterInfo.UnsetNodeInfo()

	if !plan.NumFaultsToTolerate.IsUnknown() {
		clusterInfo.SetNumFaultsToTolerate(int32(plan.NumFaultsToTolerate.ValueInt64()))
	}

	if fflags.IsFeatureFlagEnabled(fflags.MultiCloudSupport) {
		if !plan.IsMultiCloud.IsUnknown() && !plan.IsMultiCloud.IsNull() {
			clusterInfo.SetIsMultiCloud(plan.IsMultiCloud.ValueBool())
		}
	}

	// enterprise_security defaults to false (set by NewClusterInfo) when not configured.
	// On edit, the plan carries the state value, so the existing setting is preserved.
	if !plan.EnterpriseSecurity.IsUnknown() && !plan.EnterpriseSecurity.IsNull() {
		clusterInfo.SetEnterpriseSecurity(plan.EnterpriseSecurity.ValueBool())
	}

	clusterInfo.SetClusterType(openapiclient.ClusterType(clusterType))
	if clusterExists {
		clusterVersion, _ := strconv.Atoi(plan.ClusterVersion.ValueString())
		clusterInfo.SetVersion(int32(clusterVersion))
	}

	clusterSpec = openapiclient.NewClusterSpec(
		plan.ClusterName.ValueString(),
		clusterInfo,
		softwareInfo)

	clusterSpec.SetClusterRegionInfo(clusterRegionInfo)

	return clusterSpec, true, ""
}

func setBackupReplicationTargetsInEditClusterSpec(
	plan Cluster,
	clusterRegionInfo []openapiclient.ClusterRegionInfo,
) error {

	spec := plan.BackupReplicationSpec
	disabled := !isGcpBackupReplicationFeatureEnabled() ||
		spec == nil ||
		spec.GCPSpec == nil ||
		!boolValue(spec.GCPSpec.Enabled)

	// When disabled → unset everywhere
	if disabled {
		for i := range clusterRegionInfo {
			clusterRegionInfo[i].UnsetBackupReplicationGcpTarget()
		}
		return nil
	}

	switch plan.ClusterType.ValueString() {

	case "SYNCHRONOUS":
		// All regions share the same target
		target := spec.GCPSpec.SyncClusterSpec.ReplicationConfig.Target.ValueString()
		for i := range clusterRegionInfo {
			clusterRegionInfo[i].SetBackupReplicationGcpTarget(target)
		}
		return nil

	case "GEO_PARTITIONED":
		// Map each region → target
		targetMap := make(map[string]string)
		for _, cfg := range spec.GCPSpec.GeoPartitionedClusterSpec.ReplicationConfigs {
			targetMap[cfg.DesiredRegion.ValueString()] = cfg.Target.ValueString()
		}

		for i := range clusterRegionInfo {
			region := clusterRegionInfo[i].PlacementInfo.CloudInfo.Region
			clusterRegionInfo[i].SetBackupReplicationGcpTarget(targetMap[region])
		}
		return nil

	default:
		return fmt.Errorf("unsupported cluster type: %s", plan.ClusterType.ValueString())
	}
}

func getPlan(ctx context.Context, plan tfsdk.Plan, cluster *Cluster) diag.Diagnostics {
	// NOTE: currently must manually fill out each attribute due to usage of Go structs
	// Once the opt-in conversion of null or unknown values to the empty value is implemented, this can all be replaced with req.Plan.Get(ctx, &cluster)
	// See https://www.terraform.io/plugin/framework/accessing-values#conversion-rules
	// I tried implementing Unknownable instead but could not get it to work.
	var diags diag.Diagnostics

	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_id"), &cluster.ClusterID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_name"), &cluster.ClusterName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cloud_type"), &cluster.CloudType)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_type"), &cluster.ClusterType)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_region_info"), &cluster.ClusterRegionInfo)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("fault_tolerance"), &cluster.FaultTolerance)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("num_faults_to_tolerate"), &cluster.NumFaultsToTolerate)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("is_multi_cloud"), &cluster.IsMultiCloud)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("enterprise_security"), &cluster.EnterpriseSecurity)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_tier"), &cluster.ClusterTier)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cluster_allow_list_ids"), &cluster.ClusterAllowListIDs)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("restore_backup_id"), &cluster.RestoreBackupID)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("database_track"), &cluster.DatabaseTrack)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("desired_state"), &cluster.DesiredState)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("desired_connection_pooling_state"), &cluster.DesiredConnectionPoolingState)...)

	var nodeConfig *NodeConfig
	plan.GetAttribute(ctx, path.Root("node_config"), &nodeConfig)
	if nodeConfig != nil {
		diags.Append(plan.GetAttribute(ctx, path.Root("node_config"), &cluster.NodeConfig)...)
	}

	var credentials *Credentials
	plan.GetAttribute(ctx, path.Root("credentials"), &credentials)
	if credentials != nil {
		diags.Append(plan.GetAttribute(ctx, path.Root("credentials"), &cluster.Credentials)...)
	}

	diags.Append(plan.GetAttribute(ctx, path.Root("backup_schedules"), &cluster.BackupSchedules)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("cmk_spec"), &cluster.CMKSpec)...)

	backupReplicationSpec, backupDiags := getPlanBackupReplicationSpec(ctx, plan)
	diags.Append(backupDiags...)
	if backupReplicationSpec != nil {
		cluster.BackupReplicationSpec = backupReplicationSpec
	}

	return diags
}

func getPlanBackupReplicationSpec(ctx context.Context, plan tfsdk.Plan) (*BackupReplicationSpec, diag.Diagnostics) {
	var diags diag.Diagnostics

	var enabled types.Bool
	diagsEnabled := plan.GetAttribute(ctx, path.Root("backup_replication_spec").AtName("gcp_spec").AtName("enabled"), &enabled)
	diags.Append(diagsEnabled...)

	// If the enabled field is null ,it means that the path was not provided by the user

	if diags.HasError() || enabled.IsNull() || enabled.IsUnknown() {
		return nil, diags
	}

	spec := &BackupReplicationSpec{
		GCPSpec: &GcpBackupReplicationSpec{
			Enabled: enabled,
		},
	}

	var target types.String
	diagsTarget := plan.GetAttribute(ctx,
		path.Root("backup_replication_spec").AtName("gcp_spec").AtName("sync_cluster_spec").AtName("replication_config").AtName("target"),
		&target)
	diags.Append(diagsTarget...)
	if !diagsTarget.HasError() && !target.IsNull() && !target.IsUnknown() {
		spec.GCPSpec.SyncClusterSpec = &SyncClusterGcpBackupReplicationSpec{
			ReplicationConfig: &SyncClusterGcpReplicationConfig{
				Target: target,
				// Don't read computed fields - they'll be unknown
			},
		}
	}

	configs := []GeoClusterGcpReplicationConfig{}

	basePath := path.Root("backup_replication_spec").
		AtName("gcp_spec").
		AtName("geo_partitioned_cluster_spec").
		AtName("replication_configs")

	for i := 0; ; i++ {
		configPath := basePath.AtListIndex(i)

		var desiredRegion types.String
		var target types.String

		diagsRegion := plan.GetAttribute(ctx, configPath.AtName("desired_region"), &desiredRegion)
		if diagsRegion.HasError() {
			break
		}

		diagsTarget := plan.GetAttribute(ctx, configPath.AtName("target"), &target)
		if diagsTarget.HasError() {
			diags.Append(diagsTarget...)
			continue
		}

		if desiredRegion.IsNull() && target.IsNull() {
			break
		}

		config := GeoClusterGcpReplicationConfig{
			DesiredRegion: desiredRegion,
			Target:        target,
		}

		configs = append(configs, config)
	}
	if len(configs) > 0 {
		spec.GCPSpec.GeoPartitionedClusterSpec = &GeoClusterGcpBackupReplicationSpec{
			ReplicationConfigs: configs,
		}
	}

	return spec, diags
}

// fills account, project, cluster ID from state
func getIDsFromState(ctx context.Context, state tfsdk.State, cluster *Cluster) {
	state.GetAttribute(ctx, path.Root("account_id"), &cluster.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &cluster.ProjectID)
	state.GetAttribute(ctx, path.Root("cluster_id"), &cluster.ClusterID)
	state.GetAttribute(ctx, path.Root("database_track"), &cluster.DatabaseTrack)
	state.GetAttribute(ctx, path.Root("desired_state"), &cluster.DesiredState)
	state.GetAttribute(ctx, path.Root("desired_connection_pooling_state"), &cluster.DesiredConnectionPoolingState)
	state.GetAttribute(ctx, path.Root("cluster_allow_list_ids"), &cluster.ClusterAllowListIDs)
	state.GetAttribute(ctx, path.Root("cluster_region_info"), &cluster.ClusterRegionInfo)
	state.GetAttribute(ctx, path.Root("backup_schedules"), &cluster.BackupSchedules)
	state.GetAttribute(ctx, path.Root("restore_backup_id"), &cluster.RestoreBackupID)
	state.GetAttribute(ctx, path.Root("credentials"), &cluster.Credentials)
	state.GetAttribute(ctx, path.Root("backup_replication_spec"), &cluster.BackupReplicationSpec)
}

func validateCredentials(credentials *Credentials, isCreateCluster bool) bool {

	if isCreateCluster && (credentials == nil) {
		return false
	}
	if !isCreateCluster && (credentials == nil) {
		return true
	}

	commonCredentialsProvided := !credentials.Username.IsNull() && !credentials.Password.IsNull()
	commonCredentialsNotProvided := credentials.Username.IsNull() && credentials.Password.IsNull()
	ysqlCredentialsProvided := !credentials.YSQLUsername.IsNull() && !credentials.YSQLPassword.IsNull()
	ysqlCredentialsNotProvided := credentials.YSQLUsername.IsNull() && credentials.YSQLPassword.IsNull()
	ycqlCredentialsProvided := !credentials.YCQLUsername.IsNull() && !credentials.YCQLPassword.IsNull()
	ycqlCredentialsNotProvided := credentials.YCQLUsername.IsNull() && credentials.YCQLPassword.IsNull()

	if (commonCredentialsProvided && ysqlCredentialsNotProvided && ycqlCredentialsNotProvided) ||
		(ysqlCredentialsProvided && ycqlCredentialsProvided && commonCredentialsNotProvided) {
		return true
	}

	return false

}

func validateOnlyOneCMKSpec(plan *Cluster) error {
	count := 0

	if plan.CMKSpec.GCPCMKSpec != nil {
		count++
	}
	if plan.CMKSpec.AWSCMKSpec != nil {
		count++
	}
	if plan.CMKSpec.AzureCMKSpec != nil {
		count++
	}

	if count != 1 {
		return errors.New("invalid input. Only one CMK Provider out of AWS, GCP, or AZURE must be present")
	}

	return nil
}

func createCmkSpec(plan Cluster) (*openapiclient.CMKSpec, error) {
	cmkProvider := plan.CMKSpec.ProviderType.ValueString()
	cmkSpec := openapiclient.NewCMKSpec(openapiclient.CMKProviderEnum(cmkProvider))

	if err := validateOnlyOneCMKSpec(&plan); err != nil {
		return nil, err
	}

	switch cmkProvider {
	case "GCP":
		if plan.CMKSpec.GCPCMKSpec == nil {
			return nil, errors.New("provider type is GCP but GCP CMK spec is missing")
		}
		gcpKeyRingName := plan.CMKSpec.GCPCMKSpec.KeyRingName.ValueString()
		gcpKeyName := plan.CMKSpec.GCPCMKSpec.KeyName.ValueString()
		gcpLocation := plan.CMKSpec.GCPCMKSpec.Location.ValueString()
		gcpProtectionLevel := plan.CMKSpec.GCPCMKSpec.ProtectionLevel.ValueString()
		gcpServiceAccount := plan.CMKSpec.GCPCMKSpec.GcpServiceAccount
		gcpServiceAccountSpec := openapiclient.NewGCPServiceAccount(
			gcpServiceAccount.Type.ValueString(),
			gcpServiceAccount.ProjectId.ValueString(),
			"",
			gcpServiceAccount.PrivateKeyId.ValueString(),
			gcpServiceAccount.ClientEmail.ValueString(),
			gcpServiceAccount.ClientId.ValueString(),
			gcpServiceAccount.AuthUri.ValueString(),
			gcpServiceAccount.TokenUri.ValueString(),
			gcpServiceAccount.AuthProviderX509CertUrl.ValueString(),
			gcpServiceAccount.ClientX509CertUrl.ValueString(),
		)
		if !gcpServiceAccount.PrivateKey.IsNull() && !gcpServiceAccount.PrivateKey.IsUnknown() && gcpServiceAccount.PrivateKey.ValueString() != "" {
			gcpServiceAccountSpec.SetPrivateKey(gcpServiceAccount.PrivateKey.ValueString())
		}
		if !gcpServiceAccount.UniverseDomain.IsNull() && !gcpServiceAccount.UniverseDomain.IsUnknown() && gcpServiceAccount.UniverseDomain.ValueString() != "" {
			gcpServiceAccountSpec.SetUniverseDomain(gcpServiceAccount.UniverseDomain.ValueString())
		}
		gcpCmkSpec := openapiclient.NewGCPCMKSpec(gcpKeyRingName, gcpKeyName, gcpLocation, gcpProtectionLevel)
		gcpCmkSpec.SetGcpServiceAccount(*gcpServiceAccountSpec)
		cmkSpec.SetGcpCmkSpec(*gcpCmkSpec)
	case "AWS":
		if plan.CMKSpec.AWSCMKSpec == nil {
			return nil, errors.New("provider type is AWS but AWS CMK spec is missing")
		}
		awsSecretKey := plan.CMKSpec.AWSCMKSpec.SecretKey.ValueString()
		awsAccessKey := plan.CMKSpec.AWSCMKSpec.AccessKey.ValueString()
		awsArnList := make([]string, len(plan.CMKSpec.AWSCMKSpec.ARNList))

		for i, arn := range plan.CMKSpec.AWSCMKSpec.ARNList {
			awsArnList[i] = arn.ValueString()
		}

		awsCmkSpec := openapiclient.NewAWSCMKSpec(awsAccessKey, awsSecretKey, awsArnList)
		cmkSpec.SetAwsCmkSpec(*awsCmkSpec)
	case "AZURE":
		if plan.CMKSpec.AzureCMKSpec == nil {
			return nil, errors.New("provider type is AZURE but AZURE CMK spec is missing")
		}
		azureClientId := plan.CMKSpec.AzureCMKSpec.ClientID.ValueString()
		azureClientSecret := plan.CMKSpec.AzureCMKSpec.ClientSecret.ValueString()
		azureTenantId := plan.CMKSpec.AzureCMKSpec.TenantID.ValueString()
		azureKeyVaultUri := plan.CMKSpec.AzureCMKSpec.KeyVaultUri.ValueString()
		azureKeyName := plan.CMKSpec.AzureCMKSpec.KeyName.ValueString()

		azureCmkSpec := openapiclient.NewAzureCMKSpec(azureClientId, azureClientSecret, azureTenantId, azureKeyVaultUri, azureKeyName)
		cmkSpec.SetAzureCmkSpec(*azureCmkSpec)
	}

	cmkSpec.SetIsEnabled(plan.CMKSpec.IsEnabled.ValueBool())

	return cmkSpec, nil
}

func (r resourceCluster) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	// With dynamically typed vars (e.g. list(any)), cluster_region_info can be wholly
	// unknown here; decoding into []RegionInfo then fails with "unhandled unknown value".
	var clusterRegionInfoList types.List
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("cluster_region_info"), &clusterRegionInfoList)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !clusterRegionInfoList.IsNull() && !clusterRegionInfoList.IsUnknown() {
		var clusterRegionInfo []RegionInfo
		resp.Diagnostics.Append(clusterRegionInfoList.ElementsAs(ctx, &clusterRegionInfo, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		if err := validateMultiZoneSupport(clusterRegionInfo); err != nil {
			resp.Diagnostics.AddError("Invalid num_zones field", err.Error())
		}
	}

	var isMultiCloud types.Bool
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("is_multi_cloud"), &isMultiCloud)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := validateMultiCloudSupport(isMultiCloud); err != nil {
		resp.Diagnostics.AddError("Invalid is_multi_cloud field", err.Error())
	}
}

// Create a new resource
func (r resourceCluster) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}
	var accountId, message string
	var plan Cluster
	var getAccountOK bool
	resp.Diagnostics.Append(getPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Cluster Resource: Error on Get Plan")
		return
	}

	if plan.BackupReplicationSpec != nil && plan.BackupReplicationSpec.GCPSpec != nil {
		if !isGcpBackupReplicationFeatureEnabled() {
			resp.Diagnostics.AddError("GCP backup replication feature disabled", "The backup_replication_spec block is gated by the GCP_BACKUP_REPLICATION feature flag. Set YBM_FF_GCP_BACKUP_REPLICATION=true to enable it.")
			return
		}
		if err := validateGcpBackupReplicationPlan(plan); err != nil {
			resp.Diagnostics.AddError("Invalid backup_replication_spec", err.Error())
			return
		}
	}

	if !validateCredentials(plan.Credentials, true /* isCreateCluster*/) {
		resp.Diagnostics.AddError("Invalid credentials", `Please provide 'username' and 'password' 
(which would be used in common for both YSQL and YCQL) OR all of 'ysql_username',
'ysql_password', 'ycql_username' and 'ycql_password' but not a mix of both.`)
		return
	}

	if plan.NodeConfig != nil && !plan.NodeConfig.DiskSizeGb.IsNull() && !plan.NodeConfig.DiskSizeGb.IsUnknown() && !util.IsDiskSizeValid(plan.ClusterTier.ValueString(), plan.NodeConfig.DiskSizeGb.ValueInt64()) {
		resp.Diagnostics.AddError("Invalid disk size", "The disk size for a paid cluster must be at least 50 GB.")
		return
	}

	if plan.NodeConfig != nil && !plan.NodeConfig.DiskIops.IsNull() && !plan.NodeConfig.DiskIops.IsUnknown() {
		isValid, err := util.IsDiskIopsValid(plan.CloudType.ValueString(), plan.ClusterTier.ValueString(), plan.NodeConfig.DiskIops.ValueInt64())
		if !isValid {
			resp.Diagnostics.AddError("Invalid disk IOPS", err)
			return
		}
	}

	backupId := ""
	apiClient := r.p.client

	accountId, getAccountOK, message = getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	if !plan.ClusterID.IsUnknown() && !plan.ClusterID.IsNull() && plan.ClusterID.ValueString() != "" {
		resp.Diagnostics.AddError(
			"Cluster ID provided for new cluster",
			"The cluster_id was provided even though a new cluster is being created. Do not include this field in the provider when creating a cluster.",
		)
		return
	}

	for _, regionInfo := range plan.ClusterRegionInfo {
		if !regionInfo.DiskSizeGb.IsUnknown() && !util.IsDiskSizeValid(plan.ClusterTier.ValueString(), regionInfo.DiskSizeGb.ValueInt64()) {
			resp.Diagnostics.AddError("Invalid disk size in "+regionInfo.Region.ValueString(), "The disk size for a paid cluster must be at least 50 GB.")
			return
		}

		if !regionInfo.DiskIops.IsUnknown() && !regionInfo.DiskIops.IsNull() {
			isValid, err := util.IsDiskIopsValid(plan.CloudType.ValueString(), plan.ClusterTier.ValueString(), regionInfo.DiskIops.ValueInt64())
			if !isValid {
				resp.Diagnostics.AddError("Invalid disk IOPS in "+regionInfo.Region.ValueString(), err)
				return
			}
		}

		// Validate that backup replication targets are not provided during cluster creation
		if !regionInfo.BackupReplicationGCPTarget.IsNull() && !regionInfo.BackupReplicationGCPTarget.IsUnknown() && regionInfo.BackupReplicationGCPTarget.ValueString() != "" {
			resp.Diagnostics.AddError(
				"Backup replication targets not supported during cluster creation",
				"Backup replication targets can only be configured when editing existing clusters. Please remove the backup_replication_gcp_target field and try again.",
			)
			return
		}
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID ", message)
		return
	}

	clusterSpec, clusterOK, message := createClusterSpec(ctx, apiClient, accountId, projectId, plan, Cluster{}, false)
	if !clusterOK {
		resp.Diagnostics.AddError("Unable to create cluster spec", message)
		return
	}

	createClusterRequest := *openapiclient.NewCreateClusterRequest(*clusterSpec)

	encryptedCredentials := openapiclient.NewCreateClusterRequestEncryptedDbCredentialsWithDefaults()
	if plan.Credentials.Username.IsNull() {
		encryptedCredentials.SetYsql(*openapiclient.NewEncryptedDBCredentials(b64(plan.Credentials.YSQLUsername.ValueString()), b64(plan.Credentials.YSQLPassword.ValueString())))
		encryptedCredentials.SetYcql(*openapiclient.NewEncryptedDBCredentials(b64(plan.Credentials.YCQLUsername.ValueString()), b64(plan.Credentials.YCQLPassword.ValueString())))
	} else {
		encryptedCredentials.SetYsql(*openapiclient.NewEncryptedDBCredentials(b64(plan.Credentials.Username.ValueString()), b64(plan.Credentials.Password.ValueString())))
		encryptedCredentials.SetYcql(*openapiclient.NewEncryptedDBCredentials(b64(plan.Credentials.Username.ValueString()), b64(plan.Credentials.Password.ValueString())))
	}
	createClusterRequest.SetEncryptedDbCredentials(*encryptedCredentials)

	// Connection Pooling
	if !plan.DesiredConnectionPoolingState.IsUnknown() && strings.EqualFold(plan.DesiredConnectionPoolingState.ValueString(), "Enabled") {
		features := []openapiclient.CreateClusterFeatureEnum{openapiclient.CREATECLUSTERFEATUREENUM_ENABLE_CONNECTION_POOLING}
		createClusterRequest.SetFeatures(features)
	}

	var cmkSpec *openapiclient.CMKSpec

	if plan.CMKSpec != nil {
		// EAR disabled is not supported with cluster creation
		if !plan.CMKSpec.IsEnabled.ValueBool() {
			resp.Diagnostics.AddError(
				"EAR will be enabled by default.", "Cluster creation with EAR disabled is not supported.",
			)
		}
		var err error
		cmkSpec, err = createCmkSpec(plan)

		if err == nil {
			createClusterRequest.SecurityCmkSpec = *openapiclient.NewNullableCMKSpec(cmkSpec)
		} else {
			resp.Diagnostics.AddError("Error creating CMK Spec.", err.Error())
			return
		}
	}

	clusterResp, response, err := apiClient.ClusterApi.CreateCluster(ctx, accountId, projectId).CreateClusterRequest(createClusterRequest).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			resp.Diagnostics.AddError("Could not create cluster. NOTE: The length of the HTML output indicates your authentication token may be out of date. A truncated response follows:",
				errMsg[:10000])
			return
		}
		resp.Diagnostics.AddError("Could not create cluster", errMsg)
		return
	}
	clusterId := clusterResp.Data.Info.Id
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_CREATE_CLUSTER, apiClient, ctx)
		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("the cluster creation is in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for cluster creation to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "cluster creation operation failed"
		}
		resp.Diagnostics.AddError("Unable to create cluster:", msg)
		return
	}

	// read status, wait for status to be done
	retryPolicyA := retry.NewConstant(10 * time.Second)
	retryPolicyA = retry.WithMaxDuration(3600*time.Second, retryPolicyA)
	readClusterRetries = 0
	err = retry.Do(ctx, retryPolicyA, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Active") || clusterState == "Create Failed" || clusterState == "CREATE_FAILED" {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("the cluster creation is in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to create cluster ", "The operation timed out waiting for cluster creation.")
		return
	}

	// Backup_schedule
	scheduleId := ""
	description := ""
	var r1 *http.Response
	scheduleId, description, r1, err = getBackupScheduleInfoV2(ctx, apiClient, accountId, projectId, clusterId)

	if err != nil {
		resp.Diagnostics.AddError("Unable to fetch the backup schedule for the cluster "+r1.Status, "Try again")
		return
	}

	var backUpSchedules []BackupScheduleInfo
	if len(plan.BackupSchedules) > 0 {
		if len(plan.BackupSchedules) > 1 {
			resp.Diagnostics.AddError("Could not create custom backup schedule", "More than one schedules were passed")
			return
		}

		if plan.BackupSchedules[0].State.ValueString() != "" && plan.BackupSchedules[0].RetentionPeriodInDays.ValueInt64() == 0 {
			resp.Diagnostics.AddError("Could not create custom backup schedule", "Pass both state and retention period in days ")
			return
		}
		if plan.BackupSchedules[0].State.ValueString() == "" && plan.BackupSchedules[0].RetentionPeriodInDays.ValueInt64() != 0 {
			resp.Diagnostics.AddError("Could not create custom backup schedule", "Pass both state and retention period in days ")
			return
		}

		//Edit Backup Schedule
		tflog.Info(ctx, fmt.Sprintf("User defined description '%v' default description '%v'", plan.BackupSchedules[0].BackupDescription.ValueString(), description))
		newDescription := ""
		if plan.BackupSchedules[0].BackupDescription.ValueString() == "" {
			newDescription = description
		} else {
			newDescription = plan.BackupSchedules[0].BackupDescription.ValueString()
		}
		err = EditBackupSchedule(ctx, plan.BackupSchedules[0], scheduleId, newDescription, accountId, projectId, clusterId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Error duing store: ", err.Error())
			return
		}

		backupScheduleStruct := BackupScheduleInfo{
			ScheduleID: types.StringValue(scheduleId),
		}
		backUpSchedules = append(backUpSchedules, backupScheduleStruct)
	}
	if plan.BackupSchedules != nil && len(plan.BackupSchedules) == 0 {
		backupScheduleStruct := BackupScheduleInfo{}
		backUpSchedules = append(backUpSchedules, backupScheduleStruct)

	}

	var allowListIDs []string
	allowListProvided := false
	if plan.ClusterAllowListIDs != nil {
		for i := range plan.ClusterAllowListIDs {
			allowListIDs = append(allowListIDs, plan.ClusterAllowListIDs[i].ValueString())
		}

		tflog.Debug(ctx, fmt.Sprintf("Updating cluster with cluster ID %v with allow lists %v", clusterId, allowListIDs))

		_, response, err := apiClient.ClusterApi.EditClusterNetworkAllowLists(ctx, accountId, projectId, clusterId).RequestBody(allowListIDs).Execute()
		if err != nil {
			errMsg := getErrorMessage(response, err)
			resp.Diagnostics.AddError("Unable to assign allow list to cluster", errMsg)
			return
		}
		allowListProvided = true
	}

	restoreRequired := false
	if shouldUseRestoreBackupID(plan.RestoreBackupID) {
		restoreRequired = true
		backupId = plan.RestoreBackupID.ValueString()
		tflog.Warn(ctx, "restore_backup_id on ybm_cluster is deprecated; use ybm_backup_restore resource instead")
		resp.Diagnostics.AddWarning(
			"restore_backup_id is deprecated",
			"Restore via ybm_cluster restore_backup_id is deprecated. Use the ybm_backup_restore resource instead for selective keyspaces and keyspace renames. restore_backup_id on ybm_cluster will be removed in a future release.",
		)
	}
	if restoreRequired {
		err = handleRestore(ctx, accountId, projectId, clusterId, backupId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Error during store: ", err.Error())
			return
		}
	}

	if isGcpBackupReplicationFeatureEnabled() && hasGcpBackupReplicationPlan(plan.BackupReplicationSpec) {
		err = r.applyGcpBackupReplication(ctx, accountId, projectId, clusterId, plan, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Unable to configure GCP backup replication", err.Error())
			return
		}
	}

	var regions []string
	for _, regionInfo := range plan.ClusterRegionInfo {
		regions = append(regions, regionInfo.Region.ValueString())
	}

	// Pause the cluster if the desired state is set to 'Paused'
	if !plan.DesiredState.IsUnknown() && strings.EqualFold(plan.DesiredState.ValueString(), "Paused") {
		err := pauseCluster(ctx, apiClient, accountId, projectId, clusterId)
		if err != nil {
			resp.Diagnostics.AddError("Pausing the cluster Failed: ", err.Error())
		}
	}

	// Connection pooling is now enabled during cluster creation via features parameter
	// No need for post-creation enablement call

	priorDatabaseTrack := databaseTrackPriorValue(plan.DatabaseTrack)
	cluster, readOK, message := resourceClusterRead(ctx, clusterId, backUpSchedules, regions, allowListProvided, allowListIDs, false, priorDatabaseTrack, apiClient)

	// Update the State file with the unmasked creds for AWS (secret key,access) and GCP (client id,private key)
	if plan.CMKSpec != nil {
		providerType := cluster.CMKSpec.ProviderType.ValueString()
		switch providerType {
		case "AWS":
			cluster.CMKSpec.AWSCMKSpec.SecretKey = types.StringValue(cmkSpec.GetAwsCmkSpec().SecretKey)
			cluster.CMKSpec.AWSCMKSpec.AccessKey = types.StringValue(cmkSpec.GetAwsCmkSpec().AccessKey)
		case "GCP":
			if cmkSpec.GetGcpCmkSpec().GcpServiceAccount.IsSet() {
				gcpServiceAccountData := cmkSpec.GetGcpCmkSpec().GcpServiceAccount.Get()
				cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.ClientId = types.StringValue(gcpServiceAccountData.ClientId)
				if gcpServiceAccountData.GetPrivateKey() != "" {
					cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.PrivateKey = types.StringValue(gcpServiceAccountData.GetPrivateKey())
				} else {
					cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.PrivateKey = types.StringNull()
				}
			}
		case "AZURE":
			cluster.CMKSpec.AzureCMKSpec.ClientSecret = types.StringValue(cmkSpec.GetAzureCmkSpec().ClientSecret)
		}
	}

	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the cluster ", message)
		return
	}

	// Workaround: the read API currently always returns is_multi_cloud=false.
	// Until that bug is fixed (tracked separately), trust the plan value: true if
	// the plan set it to true, false otherwise.
	cluster.IsMultiCloud = types.BoolValue(plan.IsMultiCloud.ValueBool())

	// set credentials for cluster (not returned by read api)
	clusterCreds := *(plan.Credentials)
	if clusterCreds.Username.IsNull() {
		cluster.Credentials = &Credentials{
			YSQLUsername: types.StringValue(clusterCreds.YSQLUsername.ValueString()),
			YSQLPassword: types.StringValue(clusterCreds.YSQLPassword.ValueString()),
			YCQLUsername: types.StringValue(clusterCreds.YCQLUsername.ValueString()),
			YCQLPassword: types.StringValue(clusterCreds.YCQLPassword.ValueString()),
			Username:     types.StringNull(),
			Password:     types.StringNull(),
		}
	} else {
		// common credentials have been used
		cluster.Credentials = &Credentials{
			Username:     types.StringValue(clusterCreds.Username.ValueString()),
			Password:     types.StringValue(clusterCreds.Password.ValueString()),
			YSQLUsername: types.StringNull(),
			YSQLPassword: types.StringNull(),
			YCQLUsername: types.StringNull(),
			YCQLPassword: types.StringNull(),
		}
	}

	// set restore backup id for cluster (not returned by read api)
	if shouldUseRestoreBackupID(plan.RestoreBackupID) {
		cluster.RestoreBackupID = types.StringValue(plan.RestoreBackupID.ValueString())
	} else {
		cluster.RestoreBackupID = types.StringNull()
	}

	if strings.EqualFold(plan.DesiredConnectionPoolingState.ValueString(), "Enabled") || strings.EqualFold(plan.DesiredConnectionPoolingState.ValueString(), "Disabled") {
		cluster.DesiredConnectionPoolingState = types.StringValue(plan.DesiredConnectionPoolingState.ValueString())
	}

	// Update the state file with backup replication enabled parameter as provided by the user
	if plan.BackupReplicationSpec != nil && plan.BackupReplicationSpec.GCPSpec != nil {
		cluster.BackupReplicationSpec.GCPSpec.Enabled = types.BoolValue(plan.BackupReplicationSpec.GCPSpec.Enabled.ValueBool())
	}

	// We need to make sure the region order is preserved to avoid terraform treating re-order as state mismatch
	alignGcpBackupReplicationRegionOrder(&cluster, getGeoGcpReplicationRegionReferenceOrder(plan.BackupReplicationSpec))

	applyIgnoredNumNodesChanges(&cluster, plan.ClusterRegionInfo)

	diags := resp.State.Set(ctx, &cluster)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getBackupScheduleInfoV2(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string) (string, string, *http.Response, error) {

	scheduleResp, r, err := apiClient.BackupApi.ListBackupSchedulesV2(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		return "", "", r, err
	}
	list := scheduleResp.GetData()
	scheduleId := list[0].GetInfo().Id
	spec := list[0].GetSpec()
	description := spec.GetDescription()
	return scheduleId, description, nil, nil
}

func pauseCluster(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string) (err error) {

	_, response, err := apiClient.ClusterApi.PauseCluster(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			return errors.New("Could not pause the cluster. " + errMsg[:10000])
		}
		return errors.New("Could not pause the cluster. " + errMsg)

	}

	// read status, wait for status to be done
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Paused") {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("the cluster is being paused"))
	})

	if err != nil {
		return errors.New("unable to pause cluster. " + "The operation timed out waiting to pause the cluster")
	}

	return nil

}

func enableConnectionPooling(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string, numNodes int) (err error) {
	connectionPoolingOpSpec := openapiclient.NewConnectionPoolingOpSpec(openapiclient.CONNECTIONPOOLINGOPENUM_ENABLE)
	response, err := apiClient.ClusterApi.PerformConnectionPoolingOperation(ctx, accountId, projectId, clusterId).ConnectionPoolingOpSpec(*connectionPoolingOpSpec).Execute()

	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			return errors.New("Could not enable connection pooling " + errMsg[:10000])
		}
		return errors.New("Could not enable connection pooling. " + errMsg)

	}

	// read status, wait for status to be done
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	// Calculate timeout based on number of nodes: 15 minutes per node
	timeout := time.Duration(15*numNodes) * time.Minute
	retryPolicy = retry.WithMaxDuration(timeout, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Active") {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("connection Pooling is being enabled"))
	})

	if err != nil {
		return errors.New("unable to enable connection pooling " + "The operation timed out waiting to enable connection pooling")
	}

	return nil
}

func disableConnectionPooling(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string, numNodes int) (err error) {
	connectionPoolingOpSpec := openapiclient.NewConnectionPoolingOpSpec(openapiclient.CONNECTIONPOOLINGOPENUM_DISABLE)
	response, err := apiClient.ClusterApi.PerformConnectionPoolingOperation(ctx, accountId, projectId, clusterId).ConnectionPoolingOpSpec(*connectionPoolingOpSpec).Execute()

	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			return errors.New("Could not disable connection pooling " + errMsg[:10000])
		}
		return errors.New("Could not disable connection pooling. " + errMsg)

	}

	// read status, wait for status to be done
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	// Calculate timeout based on number of nodes: 15 minutes per node
	timeout := time.Duration(15*numNodes) * time.Minute
	retryPolicy = retry.WithMaxDuration(timeout, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Active") {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("connection Pooling is being disabled"))
	})

	if err != nil {
		return errors.New("unable to disable connection pooling " + "The operation timed out waiting to disable connection pooling")
	}

	return nil
}

func editClusterCmk(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string, cmkSpec openapiclient.CMKSpec) (err error) {
	_, res, err := apiClient.ClusterApi.EditClusterCMK(context.Background(), accountId, projectId, clusterId).CMKSpec(cmkSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(res, err)
		if len(errMsg) > 10000 {
			return errors.New("Could not edit the cluster CMK. " + errMsg[:10000])
		}
		return errors.New("Could not edit the cluster CMK. " + errMsg)
	}

	// read status, wait for status to be done
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Active") {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("cluster CMK is getting updated"))
	})

	if err != nil {
		return errors.New("unable to edit cluster CMK. " + "The operation timed out waiting to edit CMK")
	}

	return nil
}

func handleReadFailureWithRetries(ctx context.Context, readClusterRetries *int, maxRetries int, errMsg string) error {

	if *readClusterRetries < maxRetries {
		*readClusterRetries++
		tflog.Info(ctx, "Unable to get cluster state, retrying...")
		return retry.RetryableError(errors.New("unable to get cluster state, retrying"))
	}

	tflog.Info(ctx, "Unable to get cluster state, giving up...")
	return errors.New("Unable to get cluster state: " + errMsg)
}

func resumeCluster(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string) (err error) {

	_, response, err := apiClient.ClusterApi.ResumeCluster(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		if len(errMsg) > 10000 {
			return errors.New("Could not resume the cluster. " + errMsg[:10000])
		}
		return errors.New("Could not resume the cluster. " + errMsg)
	}

	// read status, wait for status to be done
	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Active") {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("the cluster is being resumed"))
	})

	if err != nil {
		return errors.New("unable to resume cluster. " + "The operation timed out waiting to resume the cluster")
	}

	return nil

}

func getClusterState(ctx context.Context, accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) (state string, readInfoOK bool, errorMessage string) {
	clusterResp, resp, err := apiClient.ClusterApi.GetCluster(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(resp, err)
		return "", false, errMsg
	}

	return string(clusterResp.Data.Info.GetState()), true, ""
}

func getRestoreState(ctx context.Context, accountId string, projectId string, clusterId string, backupId string, restoreId string, apiClient *openapiclient.APIClient) (state string, readInfoOK bool, errorMessage string) {
	restoreResp, resp, err := apiClient.BackupApi.GetRestore(ctx, accountId, projectId, restoreId).Execute()
	if err != nil {
		errMsg := getErrorMessage(resp, err)
		return "", false, errMsg
	}
	//ListRestores(ctx, accountId, projectId).BackupId(backupId).ClusterId(clusterId).Execute()
	return string(restoreResp.Data.Info.GetState()), true, ""
}

// Read resource information
func (r resourceCluster) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state Cluster
	getIDsFromState(ctx, req.State, &state)

	var allowListIDs []string
	allowListProvided := false
	if state.ClusterAllowListIDs != nil {
		allowListProvided = true
		for i := range state.ClusterAllowListIDs {
			allowListIDs = append(allowListIDs, state.ClusterAllowListIDs[i].ValueString())
		}
	}

	var regions []string
	for _, regionInfo := range state.ClusterRegionInfo {
		regions = append(regions, regionInfo.Region.ValueString())
	}

	var backUpSchedules []BackupScheduleInfo
	if len(state.BackupSchedules) > 0 {
		backUpSchedules = append(backUpSchedules, state.BackupSchedules[0])
	}

	priorDatabaseTrack := databaseTrackPriorValue(state.DatabaseTrack)
	cluster, readOK, message := resourceClusterRead(ctx, state.ClusterID.ValueString(), backUpSchedules, regions, allowListProvided, allowListIDs, true, priorDatabaseTrack, r.p.client)
	tflog.Debug(ctx, "Cluster Read: Allow List IDs read from API server", map[string]interface{}{
		"Allow List IDs": cluster.ClusterAllowListIDs})
	// Fetch the cmkSpec information from State (to get unmasked creds)
	var cmkSpec CMKSpec
	req.State.GetAttribute(ctx, path.Root("cmk_spec"), &cmkSpec)

	if cluster.CMKSpec != nil {
		// Unmask the creds to store in the State file
		providerType := cluster.CMKSpec.ProviderType.ValueString()
		switch providerType {
		case "AWS":
			if cmkSpec.AWSCMKSpec != nil { // cmkSpec.AWSCMKSpec is nil at the time of import
				cluster.CMKSpec.AWSCMKSpec.SecretKey = types.StringValue(cmkSpec.AWSCMKSpec.SecretKey.ValueString())
				cluster.CMKSpec.AWSCMKSpec.AccessKey = types.StringValue(cmkSpec.AWSCMKSpec.AccessKey.ValueString())
			}
		case "GCP":
			if cmkSpec.GCPCMKSpec != nil {
				cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.ClientId = types.StringValue(cmkSpec.GCPCMKSpec.GcpServiceAccount.ClientId.ValueString())
				cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.PrivateKey = types.StringValue(cmkSpec.GCPCMKSpec.GcpServiceAccount.PrivateKey.ValueString())
			}
		case "AZURE":
			if cmkSpec.AzureCMKSpec != nil {
				cluster.CMKSpec.AzureCMKSpec.ClientSecret = types.StringValue(cmkSpec.AzureCMKSpec.ClientSecret.ValueString())
			}
		}
	}

	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the cluster", message)
		return
	}

	// We need to make sure the region order is preserved to avoid terraform treating re-order as state mismatch
	alignGcpBackupReplicationRegionOrder(&cluster, getGeoGcpReplicationRegionReferenceOrder(state.BackupReplicationSpec))

	tflog.Debug(ctx, "Cluster Read: Allow List IDs read from API server", map[string]interface{}{
		"Allow List IDs": cluster.ClusterAllowListIDs})

	// set credentials for cluster (not returned by read api)
	req.State.GetAttribute(ctx, path.Root("credentials"), &cluster.Credentials)
	// set restore backup id for cluster (not returned by read api)
	req.State.GetAttribute(ctx, path.Root("restore_backup_id"), &cluster.RestoreBackupID)

	// Workaround: the read API currently always returns is_multi_cloud=false.
	// Until that bug is fixed (tracked separately), keep the value already in state.
	// It is null only on import, where the API value is all we have.
	var stateIsMultiCloud types.Bool
	req.State.GetAttribute(ctx, path.Root("is_multi_cloud"), &stateIsMultiCloud)
	if !stateIsMultiCloud.IsNull() {
		cluster.IsMultiCloud = stateIsMultiCloud
	}

	applyIgnoredNumNodesChanges(&cluster, state.ClusterRegionInfo)

	diags := resp.State.Set(ctx, &cluster)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func getClusterRegionIndex(region string, regionIndexMap map[string]int, localIndex int) (index int) {
	if len(regionIndexMap) == 0 {
		return localIndex
	}
	index, ok := regionIndexMap[region]
	if ok {
		return index
	}
	return -1
}

func readBackupScheduleInfoV2(ctx context.Context, apiClient *openapiclient.APIClient, accountId string, projectId string, clusterId string, scheduleId string) ([]BackupScheduleInfo, *http.Response, error) {
	backupScheduleResp, res, err := apiClient.BackupApi.GetBackupScheduleV2(ctx, accountId, projectId, clusterId, scheduleId).Execute()
	if err != nil {
		return nil, res, err
	}
	spec := backupScheduleResp.Data.GetSpec()
	backupScheduleInfo := make([]BackupScheduleInfo, 1)
	backupScheduleStruct := BackupScheduleInfo{

		State:                     types.StringValue(string(spec.GetState())),
		CronExpression:            types.StringValue(spec.GetCronExpression()),
		BackupDescription:         types.StringValue(spec.GetDescription()),
		RetentionPeriodInDays:     types.Int64Value(int64(spec.GetRetentionPeriodInDays())),
		TimeIntervalInDays:        types.Int64Value(int64(spec.GetTimeIntervalInDays())),
		IncrementalIntervalInMins: types.Int64Value(int64(spec.GetIncrementalIntervalInMinutes())),
		ScheduleID:                types.StringValue(scheduleId),
	}
	if backupScheduleStruct.IncrementalIntervalInMins.ValueInt64() == 0 {
		backupScheduleStruct.IncrementalIntervalInMins = types.Int64Null()
	}
	// Read use_roles from response (defaults to false if not present)
	if spec.HasUseRoles() {
		backupScheduleStruct.UseRoles = types.BoolValue(spec.GetUseRoles())
	} else {
		backupScheduleStruct.UseRoles = types.BoolValue(false)
	}
	backupScheduleInfo[0] = backupScheduleStruct

	return backupScheduleInfo, nil, nil
}

func resourceClusterRead(ctx context.Context, clusterId string, backUpSchedules []BackupScheduleInfo, regions []string, allowListProvided bool, inputAllowListIDs []string, readOnly bool, priorDatabaseTrack string, apiClient *openapiclient.APIClient) (cluster Cluster, readOK bool, errorMessage string) {
	var accountId, projectId, message string
	var getAccountOK, getProjectOK bool

	accountId, getAccountOK, message = getAccountId(context.Background(), apiClient)
	if !getAccountOK {
		return cluster, false, fmt.Sprintf("unable to get account ID %s", message)
	}

	projectId, getProjectOK, message = getProjectId(context.Background(), apiClient, accountId)
	if !getProjectOK {
		return cluster, false, fmt.Sprintf("unable to get project ID %s", message)
	}

	cluster.AccountID = types.StringValue(accountId)
	cluster.ProjectID = types.StringValue(projectId)

	clusterResp, response, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return cluster, false, errMsg
	}

	if len(backUpSchedules) > 0 {
		//Below if is used for handling empty array edge case
		if backUpSchedules[0].ScheduleID.ValueString() == "" {
			backupScheduleInfo := make([]BackupScheduleInfo, 0)
			cluster.BackupSchedules = backupScheduleInfo
		}
		if backUpSchedules[0].ScheduleID.ValueString() != "" {
			scheduleId := backUpSchedules[0].ScheduleID.ValueString()
			var backupScheduleInfo []BackupScheduleInfo
			var res *http.Response
			var err error
			backupScheduleInfo, res, err = readBackupScheduleInfoV2(ctx, apiClient, accountId, projectId, clusterId, scheduleId)
			if err != nil {
				errMsg := getErrorMessage(res, err)
				return cluster, false, errMsg
			}
			cluster.BackupSchedules = backupScheduleInfo
		}
	}

	cmkResp, cmkHttpResp, err := apiClient.ClusterApi.GetClusterCMK(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(cmkHttpResp, err)
		return cluster, false, errMsg
	}
	if cmkResp.Data != nil {
		cmkSpec := CMKSpec{}
		cmkDataSpec := cmkResp.GetData().Spec.Get()

		cmkSpec.ProviderType = types.StringValue(string(cmkDataSpec.GetProviderType()))
		cmkSpec.IsEnabled = types.BoolValue(cmkDataSpec.GetIsEnabled())

		switch cmkSpec.ProviderType.ValueString() {
		case "AWS":
			awsCMKSpec := AWSCMKSpec{
				AccessKey: types.StringValue(cmkDataSpec.GetAwsCmkSpec().AccessKey),
				SecretKey: types.StringValue(cmkDataSpec.GetAwsCmkSpec().SecretKey),
				ARNList:   []types.String{},
			}
			cmkSpec.AWSCMKSpec = &awsCMKSpec

			for _, arn := range cmkDataSpec.GetAwsCmkSpec().ArnList {
				cmkSpec.AWSCMKSpec.ARNList = append(cmkSpec.AWSCMKSpec.ARNList, types.StringValue(arn))
			}
			cluster.CMKSpec = &cmkSpec

		case "GCP":
			gcpCMKSpec := GCPCMKSpec{
				KeyRingName:     types.StringValue(cmkDataSpec.GetGcpCmkSpec().KeyRingName),
				KeyName:         types.StringValue(cmkDataSpec.GetGcpCmkSpec().KeyName),
				Location:        types.StringValue(cmkDataSpec.GetGcpCmkSpec().Location),
				ProtectionLevel: types.StringValue(cmkDataSpec.GetGcpCmkSpec().ProtectionLevel),
			}

			if cmkDataSpec.GetGcpCmkSpec().GcpServiceAccount.IsSet() {
				gcpServiceAccountData := cmkDataSpec.GetGcpCmkSpec().GcpServiceAccount.Get()
				gcpServiceAccount := GCPServiceAccount{
					Type:                    types.StringValue(gcpServiceAccountData.Type),
					ProjectId:               types.StringValue(gcpServiceAccountData.ProjectId),
					PrivateKeyId:            types.StringValue(gcpServiceAccountData.PrivateKeyId),
					ClientEmail:             types.StringValue(gcpServiceAccountData.ClientEmail),
					ClientId:                types.StringValue(gcpServiceAccountData.ClientId),
					AuthUri:                 types.StringValue(gcpServiceAccountData.AuthUri),
					TokenUri:                types.StringValue(gcpServiceAccountData.TokenUri),
					AuthProviderX509CertUrl: types.StringValue(gcpServiceAccountData.AuthProviderX509CertUrl),
					ClientX509CertUrl:       types.StringValue(gcpServiceAccountData.ClientX509CertUrl),
				}
				gcpServiceAccount.PrivateKey = types.StringValue(gcpServiceAccountData.GetPrivateKey())
				if gcpServiceAccountData.GetUniverseDomain() != "" {
					gcpServiceAccount.UniverseDomain = types.StringValue(gcpServiceAccountData.GetUniverseDomain())
				} else {
					gcpServiceAccount.UniverseDomain = types.StringNull()
				}
				gcpCMKSpec.GcpServiceAccount = gcpServiceAccount
			} else {
				gcpCMKSpec.GcpServiceAccount = emptyGCPServiceAccount()
			}

			cmkSpec.GCPCMKSpec = &gcpCMKSpec
			cluster.CMKSpec = &cmkSpec
		case "AZURE":
			azureCMKSpec := AzureCMKSpec{
				ClientID:     types.StringValue(cmkDataSpec.GetAzureCmkSpec().ClientId),
				ClientSecret: types.StringValue(cmkDataSpec.GetAzureCmkSpec().ClientSecret),
				TenantID:     types.StringValue(cmkDataSpec.GetAzureCmkSpec().TenantId),
				KeyVaultUri:  types.StringValue(cmkDataSpec.GetAzureCmkSpec().KeyVaultUri),
				KeyName:      types.StringValue(cmkDataSpec.GetAzureCmkSpec().KeyName),
			}

			cmkSpec.AzureCMKSpec = &azureCMKSpec
			cluster.CMKSpec = &cmkSpec
		}
	}

	// fill with all fields of schema except credentials - credentials are not returned by api call
	cluster.AccountID = types.StringValue(accountId)
	cluster.ProjectID = types.StringValue(projectId)
	cluster.ClusterID = types.StringValue(clusterId)
	cluster.ClusterName = types.StringValue(clusterResp.Data.Spec.Name)
	desiredState := "Active"
	if strings.EqualFold("Paused", string(clusterResp.Data.Info.GetState())) {
		desiredState = "Paused"
	}
	cluster.DesiredState = types.StringValue(desiredState)

	if clusterResp.Data.Info.GetIsConnectionPoolingEnabled() {
		cluster.DesiredConnectionPoolingState = types.StringValue("Enabled")
	} else {
		cluster.DesiredConnectionPoolingState = types.StringValue("Disabled")
	}

	cluster.ClusterType = types.StringValue(string(*clusterResp.Data.Spec.ClusterInfo.ClusterType))
	cluster.ClusterTier = types.StringValue(string(clusterResp.Data.Spec.ClusterInfo.ClusterTier))
	cluster.ClusterVersion = types.StringValue(strconv.Itoa(int(clusterResp.Data.Spec.ClusterInfo.GetVersion())))

	// set database track name (preserve deprecated alias from config/state when semantically equal)
	trackId := clusterResp.Data.Spec.SoftwareInfo.GetTrackId()
	trackName, trackNameOK, message := databaseTrackNameForState(ctx, apiClient, accountId, trackId, priorDatabaseTrack)
	if !trackNameOK {
		return cluster, false, message
	}
	cluster.DatabaseTrack = types.StringValue(trackName)

	cluster.FaultTolerance = types.StringValue(string(clusterResp.Data.Spec.ClusterInfo.FaultTolerance))
	cluster.NumFaultsToTolerate = types.Int64Value(int64(*clusterResp.Data.Spec.ClusterInfo.NumFaultsToTolerate.Get()))
	cluster.IsMultiCloud = types.BoolValue(clusterResp.Data.Spec.ClusterInfo.GetIsMultiCloud())
	cluster.EnterpriseSecurity = types.BoolValue(clusterResp.Data.Spec.ClusterInfo.GetEnterpriseSecurity())
	nodeInfo := clusterResp.Data.Spec.ClusterInfo.NodeInfo.Get()
	if nodeInfo != nil {
		if cluster.NodeConfig == nil {
			cluster.NodeConfig = &NodeConfig{
				NumCores:   types.Int64Value(int64((*nodeInfo).NumCores)),
				DiskSizeGb: types.Int64Value(int64((*nodeInfo).DiskSizeGb)),
				DiskIops:   types.Int64Value(0),
			}
		} else {
			cluster.NodeConfig.NumCores = types.Int64Value(int64((*nodeInfo).NumCores))
			cluster.NodeConfig.DiskSizeGb = types.Int64Value(int64((*nodeInfo).DiskSizeGb))
		}

		iopsPtr := (*nodeInfo).DiskIops.Get()
		if iopsPtr != nil {
			cluster.NodeConfig.DiskIops = types.Int64Value(int64(*iopsPtr))
		}
	}

	cluster.ClusterInfo.State = types.StringValue(string(clusterResp.Data.Info.GetState()))
	cluster.ClusterInfo.SoftwareVersion = types.StringValue(clusterResp.Data.Info.GetSoftwareVersion())
	cluster.ClusterInfo.CreatedTime = types.StringValue(clusterResp.Data.Info.Metadata.Get().GetCreatedOn())
	cluster.ClusterInfo.UpdatedTime = types.StringValue(clusterResp.Data.Info.Metadata.Get().GetUpdatedOn())

	// Cluster endpoints
	clusterEndpoints := make(map[string]attr.Value)
	for key, val := range clusterResp.Data.Info.Endpoints {
		clusterEndpoints[key] = types.StringValue(val)
	}
	cluster.ClusterEndpoints = types.MapValueMust(types.StringType, clusterEndpoints)

	// Cluster endpoints v2
	var clusterEndpointsV2 []ClusterEndpoint
	for _, val := range clusterResp.Data.Info.ClusterEndpoints {

		tflog.Debug(ctx, fmt.Sprintf("Cluster Endpoint %v %v %v", val.GetAccessibilityType(), val.GetHost(), val.Region))

		clusterEndpoint := ClusterEndpoint{
			AccessibilityType: types.StringValue(string(val.GetAccessibilityType())),
			Host:              types.StringValue(val.GetHost()),
			Region:            types.StringValue(val.Region),
		}
		clusterEndpointsV2 = append(clusterEndpointsV2, clusterEndpoint)
	}
	cluster.ClusterEndpointsV2 = clusterEndpointsV2

	// Cluster certificate
	certResponse, certHttpResp, err := apiClient.ClusterApi.GetConnectionCertificate(context.Background()).Execute()
	if err != nil {
		errMsg := getErrorMessage(certHttpResp, err)
		return cluster, false, errMsg
	}
	cluster.ClusterCertificate = types.StringValue(*certResponse.Data)

	// This is being done to preserve order in the region list since an order mismatch is treated as state mismatch by Terraform
	regionIndexMap := map[string]int{}
	for index, region := range regions {
		regionIndexMap[region] = index
	}

	respClusterRegionInfo := clusterResp.Data.Spec.ClusterRegionInfo
	clusterRegionInfo := make([]RegionInfo, len(respClusterRegionInfo))
	// A slot no region maps to keeps v0.10's known zero values rather than v1's nulls.
	for i := range clusterRegionInfo {
		clusterRegionInfo[i] = zeroRegionInfo()
	}
	for localIndex, info := range respClusterRegionInfo {
		region := info.PlacementInfo.CloudInfo.GetRegion()
		destIndex := getClusterRegionIndex(region, regionIndexMap, localIndex)
		if destIndex < len(respClusterRegionInfo) {
			vpcID := info.PlacementInfo.GetVpcId()
			vpcName := ""
			if vpcID != "" {
				vpcData, err := getVPCByID(context.Background(), accountId, projectId, info.PlacementInfo.GetVpcId(), apiClient)
				if err != nil {
					return cluster, false, err.Error()
				}
				vpcName = vpcData.Spec.Name
			}

			// if info.AccessibilityTypes contains "PUBLIC" then set PublicAccess to true
			publicAccess := false
			for _, accessibilityType := range info.GetAccessibilityTypes() {
				if accessibilityType == "PUBLIC" {
					publicAccess = true
					break
				}
			}

			tflog.Debug(ctx, fmt.Sprintf("For region %v, publicAccess = %v", region, publicAccess))

			var backupReplicationGCPTarget types.String
			if info.HasBackupReplicationGcpTarget() && len(info.GetBackupReplicationGcpTarget()) > 0 {
				backupReplicationGCPTarget = types.StringValue(info.GetBackupReplicationGcpTarget())
			} else {
				backupReplicationGCPTarget = types.StringNull()
			}

			// Handle backup region - get from cluster_region_info_details
			backupRegion := types.BoolValue(false)
			// Find the corresponding region info details to get backup_region
			for _, regionDetail := range clusterResp.Data.Info.ClusterRegionInfoDetails {
				if regionDetail.Region == region {
					backupRegion = types.BoolValue(regionDetail.BackupRegion)
					break
				}
			}

			var numZones types.Int64
			if fflags.IsFeatureFlagEnabled(fflags.MultiZoneSupport) && info.PlacementInfo.HasNumZones() {
				numZones = types.Int64Value(int64(info.PlacementInfo.GetNumZones()))
			} else {
				numZones = types.Int64Null()
			}

			regionInfo := RegionInfo{
				Region:                     types.StringValue(region),
				NumNodes:                   types.Int64Value(int64(info.PlacementInfo.GetNumNodes())),
				NumZones:                   numZones,
				NumCores:                   types.Int64Value(int64(info.NodeInfo.Get().GetNumCores())),
				DiskSizeGb:                 types.Int64Value(int64(info.NodeInfo.Get().GetDiskSizeGb())),
				DiskIops:                   types.Int64Value(int64(info.NodeInfo.Get().GetDiskIops())),
				VPCID:                      types.StringValue(vpcID),
				VPCName:                    types.StringValue(vpcName),
				PublicAccess:               types.BoolValue(publicAccess),
				IsPreferred:                types.BoolValue(info.GetIsAffinitized()),
				IsDefault:                  types.BoolValue(info.GetIsDefault()),
				BackupReplicationGCPTarget: backupReplicationGCPTarget,
				BackupRegion:               backupRegion,
				IgnoreNumNodesChanges:      types.BoolValue(false),
			}
			clusterRegionInfo[destIndex] = regionInfo
		}
	}
	cluster.ClusterRegionInfo = clusterRegionInfo
	cluster.CloudType = types.StringValue(string(respClusterRegionInfo[0].PlacementInfo.CloudInfo.GetCode()))

	if cluster.CloudType.ValueString() == "GCP" && cluster.ClusterTier.ValueString() == "PAID" {
		backupSpec, err := readGcpBackupReplicationState(ctx, apiClient, accountId, projectId, clusterId, cluster.ClusterType.ValueString())
		if err != nil {
			return cluster, false, err.Error()
		}
		cluster.BackupReplicationSpec = backupSpec
	} else {
		cluster.BackupReplicationSpec = nil
	}

	if allowListProvided {
		const maxRetries = 5
		for attempt := 0; attempt < maxRetries; attempt++ {
			clusterAllowListMappingResp, response, err := apiClient.ClusterApi.ListClusterNetworkAllowLists(ctx, accountId, projectId, clusterId).Execute()
			if err != nil {
				errMsg := getErrorMessage(response, err)
				return cluster, false, errMsg
			}
			allowListIDMap := map[string]bool{}
			var allowListIDs []types.String
			var allowListStrings []string
			// This is being done to preserve order in the list since an order mismatch is treated as state mismatch by Terraform
			for _, elem := range clusterAllowListMappingResp.Data {
				allowListIDMap[elem.Info.Id] = true
			}
			if !readOnly {
				for _, elem := range inputAllowListIDs {
					if _, ok := allowListIDMap[elem]; ok {
						allowListStrings = append(allowListStrings, elem)
					}
				}
				tflog.Debug(ctx, fmt.Sprintf("Attempt %d: Input Allow List is %v, Server Allow List is %v", attempt+1, inputAllowListIDs, allowListStrings))
				//added len(inputAllowListIDs)==0 in if condition so that we can reuse the func resourceClusterRead in data_source_cluster_name.go.
				if util.AreListsEqual(allowListStrings, inputAllowListIDs) || len(inputAllowListIDs) == 0 {
					for _, elem := range allowListStrings {
						allowListIDs = append(allowListIDs, types.StringValue(elem))
					}
					cluster.ClusterAllowListIDs = allowListIDs
					return cluster, true, ""
				}
			}
			if readOnly {
				for _, elem := range clusterAllowListMappingResp.Data {
					allowListStrings = append(allowListStrings, elem.Info.Id)
				}
				if len(inputAllowListIDs) != 0 {
					for _, elem := range allowListStrings {
						allowListIDs = append(allowListIDs, types.StringValue(elem))
					}
					cluster.ClusterAllowListIDs = allowListIDs
				}
				return cluster, true, ""
			}

			// Don't sleep on the last attempt
			if attempt < maxRetries-1 {
				time.Sleep(10 * time.Second)
			}
		}

		// All retries exhausted
		tflog.Error(context.Background(), fmt.Sprintf("Failed to synchronize allow lists after %d attempts", maxRetries))
		return cluster, false, "Failed to synchronize allow lists after maximum retry attempts"
	}

	return cluster, true, ""
}

func getClusterVersion(accountId string, projectId string, clusterId string, apiClient *openapiclient.APIClient) (version int, readOK bool, errorMessage string) {
	clusterResp, response, err := apiClient.ClusterApi.GetCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return 0, false, errMsg
	}

	return int(clusterResp.Data.Spec.ClusterInfo.GetVersion()), true, ""
}

func handleRestore(ctx context.Context, accountId string, projectId string, clusterId string, backupId string, apiClient *openapiclient.APIClient) error {
	restoreSpec := *openapiclient.NewRestoreSpec()
	restoreSpec.SetBackupId(backupId)
	restoreSpec.SetClusterId(clusterId)
	tflog.Debug(ctx, fmt.Sprintf("Restoring to cluster with cluster ID %v the backup with backup ID %v", clusterId, backupId))

	restoreResp, response, err := apiClient.BackupApi.RestoreBackup(ctx, accountId, projectId).RestoreSpec(restoreSpec).Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return errors.New("Unable to restore backup to cluster: " + errMsg)
	}

	restoreId := *restoreResp.Data.Info.Id
	// read status, wait for status to be done
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(1200*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		restoreState, readInfoOK, message := getRestoreState(ctx, accountId, projectId, clusterId, backupId, restoreId, apiClient)
		if readInfoOK {
			if restoreState == "SUCCEEDED" {
				return nil
			}
		} else {
			return retry.RetryableError(errors.New("Unable to get restore state: " + message))
		}
		return retry.RetryableError(errors.New("the backup restore is in progress"))
	})

	if err != nil {
		return errors.New("unable to restore backup to the cluster: The operation timed out waiting for backup restore")
	}

	return nil
}

// Update resource
func (r resourceCluster) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan Cluster
	resp.Diagnostics.Append(getPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.BackupReplicationSpec != nil && plan.BackupReplicationSpec.GCPSpec != nil {
		if !isGcpBackupReplicationFeatureEnabled() {
			resp.Diagnostics.AddError("GCP backup replication feature disabled", "The backup_replication_spec block is gated by the GCP_BACKUP_REPLICATION feature flag. Set YBM_FF_GCP_BACKUP_REPLICATION=true to enable it.")
			return
		}
		if err := validateGcpBackupReplicationPlan(plan); err != nil {
			resp.Diagnostics.AddError("Invalid backup_replication_spec", err.Error())
			return
		}
	}

	if plan.NodeConfig != nil && !plan.NodeConfig.DiskSizeGb.IsUnknown() && !util.IsDiskSizeValid(plan.ClusterTier.ValueString(), plan.NodeConfig.DiskSizeGb.ValueInt64()) {
		resp.Diagnostics.AddError("Invalid disk size", "The disk size for a paid cluster must be at least 50 GB.")
		return
	}

	if plan.NodeConfig != nil && !plan.NodeConfig.DiskIops.IsNull() && !plan.NodeConfig.DiskIops.IsUnknown() {
		isValid, err := util.IsDiskIopsValid(plan.CloudType.ValueString(), plan.ClusterTier.ValueString(), plan.NodeConfig.DiskIops.ValueInt64())
		if !isValid {
			resp.Diagnostics.AddError("Invalid disk IOPS", err)
			return
		}
	}

	apiClient := r.p.client
	var state Cluster
	getIDsFromState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	projectId := state.ProjectID.ValueString()
	clusterId := state.ClusterID.ValueString()

	// Credentials can not be modified after creation.
	if plan.Credentials != nil {
		if state.Credentials == nil || *(state.Credentials) != *(plan.Credentials) {
			resp.Diagnostics.AddError(
				"Credentials cannot be modified after creation",
				"Database credentials can be set only at the time of creation and cannot be modified after creation.")
			return
		}
	}

	// is_multi_cloud can not be modified after creation.
	var stateIsMultiCloud types.Bool
	req.State.GetAttribute(ctx, path.Root("is_multi_cloud"), &stateIsMultiCloud)
	if !stateIsMultiCloud.IsNull() && !stateIsMultiCloud.IsUnknown() &&
		!plan.IsMultiCloud.IsNull() && !plan.IsMultiCloud.IsUnknown() &&
		stateIsMultiCloud.ValueBool() != plan.IsMultiCloud.ValueBool() {
		resp.Diagnostics.AddError(
			"is_multi_cloud cannot be modified after creation",
			"The 'is_multi_cloud' field can be set only at the time of creation and cannot be modified after creation.")
		return
	}

	// enterprise_security can not be modified after creation.
	var stateEnterpriseSecurity types.Bool
	req.State.GetAttribute(ctx, path.Root("enterprise_security"), &stateEnterpriseSecurity)
	if !stateEnterpriseSecurity.IsNull() && !stateEnterpriseSecurity.IsUnknown() &&
		!plan.EnterpriseSecurity.IsNull() && !plan.EnterpriseSecurity.IsUnknown() &&
		stateEnterpriseSecurity.ValueBool() != plan.EnterpriseSecurity.ValueBool() {
		resp.Diagnostics.AddError(
			"enterprise_security cannot be modified after creation",
			"The 'enterprise_security' field can be set only at the time of creation and cannot be modified after creation.")
		return
	}

	// Resume the cluster if the desired state is set to 'Active' and it is paused currently
	if strings.EqualFold(state.DesiredState.ValueString(), "Paused") && (plan.DesiredState.IsUnknown() || strings.EqualFold(plan.DesiredState.ValueString(), "Active")) {
		// Resume the cluster
		tflog.Debug(ctx, "The cluster has been resumed successfully")
		err := resumeCluster(ctx, apiClient, accountId, projectId, clusterId)
		if err != nil {
			resp.Diagnostics.AddError("Cluster update failed: ", err.Error())
			return
		}
	}

	// Calculate total number of nodes across all regions for connection pooling timeout
	totalNodes := 0
	for _, regionInfo := range plan.ClusterRegionInfo {
		totalNodes += int(regionInfo.NumNodes.ValueInt64())
	}

	// Disable Connection Pooling if the desired state is set to 'Disabled' and it is enabled currently
	if !plan.DesiredConnectionPoolingState.IsUnknown() && !state.DesiredConnectionPoolingState.IsUnknown() && strings.EqualFold(state.DesiredConnectionPoolingState.ValueString(), "Enabled") && strings.EqualFold(plan.DesiredConnectionPoolingState.ValueString(), "Disabled") {
		// Disable Connection Pooling
		tflog.Info(ctx, fmt.Sprintf("Existing Desired Connection Pooling State in State is %v", state.DesiredConnectionPoolingState.ValueString()))
		err := disableConnectionPooling(ctx, apiClient, accountId, projectId, clusterId, totalNodes)
		if err != nil {
			resp.Diagnostics.AddError("Disable connection pooling failed: ", err.Error())
			return
		}
	}

	for _, regionInfo := range plan.ClusterRegionInfo {
		if !regionInfo.DiskSizeGb.IsUnknown() && !util.IsDiskSizeValid(plan.ClusterTier.ValueString(), regionInfo.DiskSizeGb.ValueInt64()) {
			resp.Diagnostics.AddError("Invalid disk size in "+regionInfo.Region.ValueString(), "The disk size for a paid cluster must be at least 50 GB.")
			return
		}

		if !(regionInfo.DiskIops.IsUnknown() || regionInfo.DiskIops.IsNull()) {
			isValid, err := util.IsDiskIopsValid(plan.CloudType.ValueString(), plan.ClusterTier.ValueString(), regionInfo.DiskIops.ValueInt64())
			if !isValid {
				resp.Diagnostics.AddError("Invalid disk IOPS in "+regionInfo.Region.ValueString(), err)
				return
			}
		}
	}

	scheduleId := ""
	backupDescription := ""
	var r1 *http.Response
	var err error

	scheduleId, backupDescription, r1, err = getBackupScheduleInfoV2(ctx, apiClient, accountId, projectId, clusterId)
	if err != nil {
		resp.Diagnostics.AddError("Unable to fetch the backup schedule for the cluster "+r1.Status, "Try again")
		return
	}

	clusterSpec, clusterOK, message := createClusterSpec(ctx, apiClient, accountId, projectId, plan, state, true)
	if !clusterOK {
		resp.Diagnostics.AddError("Unable to create cluster specification ", message)
		return
	}

	clusterVersion, versionOK, message := getClusterVersion(accountId, projectId, clusterId, apiClient)
	if !versionOK {
		resp.Diagnostics.AddError("Unable to get cluster version ", message)
		return
	}
	clusterSpec.ClusterInfo.SetVersion(int32(clusterVersion))
	maxRetries := 5
	var response *http.Response

	for {
		_, response, err = apiClient.ClusterApi.EditCluster(ctx, accountId, projectId, clusterId).ClusterSpec(*clusterSpec).Execute()
		if err != nil {
			// Retry on 503s
			if response != nil && response.StatusCode == 503 && maxRetries > 0 {
				maxRetries--
				tflog.Warn(ctx, "Received 503 Service Unavailable. Retrying after 10 sec...")
				time.Sleep(10 * time.Second)
				continue
			}
			errMsg := getErrorMessage(response, err)
			if len(errMsg) > 10000 {
				resp.Diagnostics.AddError("Unable to edit cluster. NOTE: The length of the HTML output indicates your authentication token may be out of date. A truncated response follows: ",
					errMsg[:10000])
				return
			}
			resp.Diagnostics.AddError("Unable to edit cluster ", errMsg)
			return
		}
		break
	}

	// The following code has a pitfall:
	// If we change just the cluster_allow_list_ids field, then we will send a cluster edit
	// request to the server. The server will see the spec is the same as the current spec,
	// so there will be no task submitted.
	// If there is no task submitted (EVER), we will get a TASK_NOT_FOUND.
	// If there was EVER a task submitted, we will get the status of that task (likely SUCCESS).
	//
	// Challenges:
	// 1. Last EDIT was not successful - the customer should first perform an edit to get out of that state.
	// 2. To work around ANY possible race condition in the server side (task created AFTER the response),
	// we will try twice to read the task state. If both times we can't find the task, we bail out.
	//
	// Something similar will happen if changing the backup schedule or the CMK spec.
	retries := 0
	readClusterRetries := 0
	checkNewTaskSpawned := true
	retryPolicy := retry.NewConstant(10 * time.Second)
	editTimeout := time.Duration(max(60, 30*totalNodes)) * time.Minute // 30 minutes per node, Min. 60 minutes
	retryPolicy = retry.WithMaxDuration(editTimeout, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_EDIT_CLUSTER, apiClient, ctx)

		tflog.Info(ctx, "Cluster edit operation in progress, state: "+asState)

		if readInfoOK {
			if asState == "TASK_NOT_FOUND" {
				// We try for a minute waiting for the tasks to be spawned. If edit cluster responded with a success
				// without creating a task for about a minute, we can safely assume that a task is not required to be spawned.
				// We also test for the cluster to be in an active state in that code that follows. So, we can safely do this.
				if retries < 6 {
					retries++
					tflog.Info(ctx, "Cluster edit task not found, retrying...")
					return retry.RetryableError(errors.New("cluster not found, retrying"))
				} else {
					tflog.Info(ctx, "Cluster edit task not found, the change would not have required a task creation")
					return nil
				}
			}
			// There are cases this code flow checks for the state of previously spawned tasks instead of checking for new tasks.
			// Hence, we check whether a new task is spawned.
			if checkNewTaskSpawned {
				if asState == string(openapiclient.TASKACTIONSTATEENUM_IN_PROGRESS) {
					checkNewTaskSpawned = false
					return retry.RetryableError(errors.New("cluster edit operation in progress"))
				} else {
					tflog.Info(ctx, "Cluster edit task not found, the change would not have required a task creation")
					return nil
				}
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}

		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("cluster edit operation in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for cluster edit to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "cluster edit operation failed"
		}
		resp.Diagnostics.AddError("Unable to edit cluster:", msg)
		return
	}

	// read status, wait for status to be active
	readClusterRetries = 0
	retryPolicyA := retry.NewConstant(10 * time.Second)
	retryPolicyA = retry.WithMaxDuration(editTimeout, retryPolicyA)
	err = retry.Do(ctx, retryPolicyA, func(ctx context.Context) error {
		clusterState, readInfoOK, message := getClusterState(ctx, accountId, projectId, clusterId, apiClient)
		if readInfoOK {
			if strings.EqualFold(clusterState, "Active") || clusterState == "Create Failed" || clusterState == "CREATE_FAILED" {
				return nil
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("cluster edit is in progress"))
	})

	if err != nil {
		resp.Diagnostics.AddError("Unable to edit cluster:", "The operation timed out waiting for cluster edit to complete.")
		return
	}

	var backUpSchedules []BackupScheduleInfo
	if len(plan.BackupSchedules) > 0 {
		if len(plan.BackupSchedules) > 1 {
			resp.Diagnostics.AddError("Could not create custom backup schedule", "More than one schedules were passed")
			return
		}
		if plan.BackupSchedules[0].State.ValueString() != "" && plan.BackupSchedules[0].RetentionPeriodInDays.ValueInt64() == 0 {
			resp.Diagnostics.AddError("Unable to modify backup schedule", "You must provide both state and retention period in days.")
			return
		}
		if plan.BackupSchedules[0].State.ValueString() == "" && plan.BackupSchedules[0].RetentionPeriodInDays.ValueInt64() != 0 {
			resp.Diagnostics.AddError("Unable to modify backup schedule", "You must provide both state and retention period in days.")
			return
		}
		tflog.Info(ctx, fmt.Sprintf("User defined description '%v' default description '%v'", plan.BackupSchedules[0].BackupDescription.ValueString(), backupDescription))
		newDescription := ""

		if plan.BackupSchedules[0].BackupDescription.ValueString() == "" {
			newDescription = backupDescription
		} else {
			newDescription = plan.BackupSchedules[0].BackupDescription.ValueString()
		}
		err = EditBackupSchedule(ctx, plan.BackupSchedules[0], scheduleId, newDescription, accountId, projectId, clusterId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Error duing store: ", err.Error())
			return
		}

		backupScheduleStruct := BackupScheduleInfo{
			ScheduleID: types.StringValue(scheduleId),
		}
		backUpSchedules = append(backUpSchedules, backupScheduleStruct)
	}

	if plan.BackupSchedules != nil && len(plan.BackupSchedules) == 0 {
		backupScheduleStruct := BackupScheduleInfo{}
		backUpSchedules = append(backUpSchedules, backupScheduleStruct)

	}

	if plan.CMKSpec != nil {
		cmkSpec, err := createCmkSpec(plan)
		if err != nil {
			resp.Diagnostics.AddError("Error creating CMK Spec: ", err.Error())
			return
		} else {
			err = editClusterCmk(ctx, apiClient, accountId, projectId, clusterId, *cmkSpec)
			if err != nil {
				resp.Diagnostics.AddError("Cluster CMK update failed: ", err.Error())
				return
			}
		}
	}

	var allowListIDs []string
	allowListProvided := false

	if plan.ClusterAllowListIDs != nil {
		for i := range plan.ClusterAllowListIDs {
			allowListIDs = append(allowListIDs, plan.ClusterAllowListIDs[i].ValueString())
		}

		_, response, err := apiClient.ClusterApi.EditClusterNetworkAllowLists(context.Background(), accountId, projectId, clusterId).RequestBody(allowListIDs).Execute()
		if err != nil {
			errMsg := getErrorMessage(response, err)
			resp.Diagnostics.AddError("Unable to assign allow list to cluster ", errMsg)
			return
		}
		allowListProvided = true
	}

	tflog.Debug(ctx, "Cluster Update: Details about allow list IDs", map[string]interface{}{
		"Allow List IDs":  allowListIDs,
		"Provided or Not": allowListProvided})

	restoreRequired := false
	backupId := ""
	planHasRestoreBackupID := shouldUseRestoreBackupID(plan.RestoreBackupID)
	if planHasRestoreBackupID {
		tflog.Warn(ctx, "restore_backup_id on ybm_cluster is deprecated; use ybm_backup_restore resource instead")
		resp.Diagnostics.AddWarning(
			"restore_backup_id is deprecated",
			"Restore via ybm_cluster restore_backup_id is deprecated. Use the ybm_backup_restore resource instead. restore_backup_id on ybm_cluster will be removed in a future release.",
		)
		if state.RestoreBackupID.IsNull() || state.RestoreBackupID.ValueString() != plan.RestoreBackupID.ValueString() {
			restoreRequired = true
		}
		backupId = plan.RestoreBackupID.ValueString()
	}
	if restoreRequired {
		err = handleRestore(ctx, accountId, projectId, clusterId, backupId, apiClient)
		if err != nil {
			resp.Diagnostics.AddError("Error during store: ", err.Error())
			return
		}
	}

	// Pause the cluster if the desired state is set to 'Paused' and it is active currently
	if strings.EqualFold(state.DesiredState.ValueString(), "Active") && (!plan.DesiredState.IsUnknown() && strings.EqualFold(plan.DesiredState.ValueString(), "Paused")) {
		// Pause the cluster
		err := pauseCluster(ctx, apiClient, accountId, projectId, clusterId)
		if err != nil {
			resp.Diagnostics.AddError("Cluster update failed: ", err.Error())
			return
		}
	}

	// Enable connection pooling if the desired state is set to 'Enabled' and it is not already enabled
	if !plan.DesiredConnectionPoolingState.IsUnknown() && !state.DesiredConnectionPoolingState.IsUnknown() && strings.EqualFold(state.DesiredConnectionPoolingState.ValueString(), "Disabled") && strings.EqualFold(plan.DesiredConnectionPoolingState.ValueString(), "Enabled") {
		err := enableConnectionPooling(ctx, apiClient, accountId, projectId, clusterId, totalNodes)
		if err != nil {
			resp.Diagnostics.AddError("Enabling connection pooling Failed: ", err.Error())
		}
	}

	var regions []string
	for _, regionInfo := range plan.ClusterRegionInfo {
		regions = append(regions, regionInfo.Region.ValueString())
	}

	priorDatabaseTrack := databaseTrackPriorValue(plan.DatabaseTrack)
	if priorDatabaseTrack == "" {
		priorDatabaseTrack = databaseTrackPriorValue(state.DatabaseTrack)
	}
	cluster, readOK, message := resourceClusterRead(ctx, clusterId, backUpSchedules, regions, allowListProvided, allowListIDs, false, priorDatabaseTrack, apiClient)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the cluster ", message)
		return
	}
	tflog.Debug(ctx, "Cluster Update: Allow list IDs read from API server ", map[string]interface{}{
		"Allow List IDs": cluster.ClusterAllowListIDs})

	// Workaround: the read API currently always returns is_multi_cloud=false.
	// Until that bug is fixed (tracked separately), trust the plan value: true if
	// the plan set it to true, false otherwise.
	cluster.IsMultiCloud = types.BoolValue(plan.IsMultiCloud.ValueBool())

	// Update the State file with the unmasked creds for AWS (Secret Key, Access Key), GCP (Client ID, Private Key)
	// and Azure (client ID, client Secret, tenant ID)
	if plan.CMKSpec != nil {
		providerType := cluster.CMKSpec.ProviderType.ValueString()
		switch providerType {
		case "AWS":
			cluster.CMKSpec.AWSCMKSpec.SecretKey = plan.CMKSpec.AWSCMKSpec.SecretKey
			cluster.CMKSpec.AWSCMKSpec.AccessKey = plan.CMKSpec.AWSCMKSpec.AccessKey
		case "GCP":
			cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.ClientId = plan.CMKSpec.GCPCMKSpec.GcpServiceAccount.ClientId
			cluster.CMKSpec.GCPCMKSpec.GcpServiceAccount.PrivateKey = plan.CMKSpec.GCPCMKSpec.GcpServiceAccount.PrivateKey
		case "AZURE":
			cluster.CMKSpec.AzureCMKSpec.ClientSecret = plan.CMKSpec.AzureCMKSpec.ClientSecret
		}
	}

	// Set credentials
	if plan.Credentials == nil {
		// Cluster imported or credentials removed after creation
		cluster.Credentials = nil
	} else {
		// set credentials for cluster (not returned by read api)
		req.State.GetAttribute(ctx, path.Root("credentials"), &cluster.Credentials)
	}

	// set restore backup id for cluster (not returned by read api)
	if planHasRestoreBackupID {
		cluster.RestoreBackupID = types.StringValue(plan.RestoreBackupID.ValueString())
	} else if plan.RestoreBackupID.IsUnknown() {
		cluster.RestoreBackupID = state.RestoreBackupID
	} else {
		cluster.RestoreBackupID = types.StringNull()
	}

	if strings.EqualFold(plan.DesiredConnectionPoolingState.ValueString(), "Enabled") || strings.EqualFold(plan.DesiredConnectionPoolingState.ValueString(), "Disabled") {
		cluster.DesiredConnectionPoolingState = types.StringValue(plan.DesiredConnectionPoolingState.ValueString())
	}

	// We need to make sure the region order is preserved to avoid terraform treating re-order as state mismatch
	alignGcpBackupReplicationRegionOrder(&cluster, getGeoGcpReplicationRegionReferenceOrder(plan.BackupReplicationSpec))

	applyIgnoredNumNodesChanges(&cluster, plan.ClusterRegionInfo)

	diags := resp.State.Set(ctx, &cluster)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete resource
func (r resourceCluster) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state Cluster
	getIDsFromState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	projectId := state.ProjectID.ValueString()
	clusterId := state.ClusterID.ValueString()

	apiClient := r.p.client

	_, err := apiClient.ClusterApi.DeleteCluster(context.Background(), accountId, projectId, clusterId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete the cluster ", GetApiErrorDetails(err))
		return
	}

	readClusterRetries := 0
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err = retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		asState, readInfoOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_DELETE_CLUSTER, apiClient, ctx)

		tflog.Info(ctx, "Cluster delete operation in progress, state: "+asState)

		if readInfoOK {
			if asState == string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED) {
				return nil
			}
			if asState == string(openapiclient.TASKACTIONSTATEENUM_FAILED) {
				return ErrFailedTask
			}
		} else {
			return handleReadFailureWithRetries(ctx, &readClusterRetries, 2, message)
		}
		return retry.RetryableError(errors.New("cluster deletion operation in progress"))
	})

	if err != nil {
		msg := "The operation timed out waiting for cluster deletion to complete."
		if errors.Is(err, ErrFailedTask) {
			msg = "cluster deletion operation failed"
		}
		resp.Diagnostics.AddError("Unable to delete cluster:", msg)
		return
	}
	resp.State.RemoveResource(ctx)
}

// ImportState Import resource
func (r resourceCluster) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Save the import identifier in the cluster_id attribute
	resource.ImportStatePassthroughID(ctx, path.Root("cluster_id"), req, resp)
}

func validateMultiZoneSupport(clusterRegionInfo []RegionInfo) error {
	for _, regionInfo := range clusterRegionInfo {
		if !regionInfo.NumZones.IsNull() && !regionInfo.NumZones.IsUnknown() && !fflags.IsFeatureFlagEnabled(fflags.MultiZoneSupport) {
			return fmt.Errorf(
				"Multi-zone support feature disabled: The 'num_zones' field is set in cluster_region_info, but the MULTI_ZONE_SUPPORT feature flag is disabled. Set YBM_FF_MULTI_ZONE_SUPPORT=true to enable it.",
			)
		}
	}
	return nil
}

func validateMultiCloudSupport(isMultiCloud types.Bool) error {
	if !isMultiCloud.IsNull() && !isMultiCloud.IsUnknown() && isMultiCloud.ValueBool() &&
		!fflags.IsFeatureFlagEnabled(fflags.MultiCloudSupport) {
		return fmt.Errorf(
			"Multi-cloud support feature disabled: The 'is_multi_cloud' field is set to true, but the MULTI_CLOUD_SUPPORT feature flag is disabled. Set YBM_FF_MULTI_CLOUD_SUPPORT=true to enable it.",
		)
	}
	return nil
}

func isGcpBackupReplicationFeatureEnabled() bool {
	return fflags.IsFeatureFlagEnabled(fflags.GCPBackupReplication)
}

func hasGcpBackupReplicationPlan(spec *BackupReplicationSpec) bool {
	return spec != nil && spec.GCPSpec != nil
}

func validateGcpBackupReplicationPlan(plan Cluster) error {
	if plan.BackupReplicationSpec == nil || plan.BackupReplicationSpec.GCPSpec == nil {
		return nil
	}

	if plan.CloudType.ValueString() != "GCP" {
		return fmt.Errorf("backup_replication_spec is supported only for GCP clusters")
	}

	if strings.EqualFold(plan.ClusterTier.ValueString(), "FREE") {
		return fmt.Errorf("backup_replication_spec is not supported for free tier clusters")
	}

	gcpSpec := plan.BackupReplicationSpec.GCPSpec
	syncSpec := gcpSpec.SyncClusterSpec
	geoSpec := gcpSpec.GeoPartitionedClusterSpec

	if boolValue(gcpSpec.Enabled) {
		switch plan.ClusterType.ValueString() {
		case "SYNCHRONOUS":
			if syncSpec == nil || syncSpec.ReplicationConfig == nil {
				return fmt.Errorf("sync_cluster_spec.replication_config must be provided for synchronous clusters")
			}
		case "GEO_PARTITIONED":
			if geoSpec == nil || len(geoSpec.ReplicationConfigs) == 0 {
				return fmt.Errorf("geo_partitioned_cluster_spec.replication_configs must be provided for geo partitioned clusters")
			}
			clusterRegions := make(map[string]struct{})
			for _, region := range plan.ClusterRegionInfo {
				regionName := strings.TrimSpace(typesStringValue(region.Region))
				if regionName != "" {
					clusterRegions[regionName] = struct{}{}
				}
			}
			if len(geoSpec.ReplicationConfigs) != len(clusterRegions) {
				return fmt.Errorf("geo_partitioned_cluster_spec must include a replication_config for every cluster region")
			}
			for _, cfg := range geoSpec.ReplicationConfigs {
				regionName := strings.TrimSpace(typesStringValue(cfg.DesiredRegion))
				if _, ok := clusterRegions[regionName]; !ok {
					return fmt.Errorf("desired_region %s must match one of the cluster regions", regionName)
				}
			}
		default:
			return fmt.Errorf("backup_replication_spec is not supported for cluster type %s", plan.ClusterType.ValueString())
		}
	} else {
		if syncSpec != nil || geoSpec != nil {
			return fmt.Errorf("backup_replication_spec.gcp_spec.enabled must be true when sync_cluster_spec or geo_partitioned_cluster_spec is set")
		}
	}

	return nil
}

func (r resourceCluster) applyGcpBackupReplication(ctx context.Context, accountId, projectId, clusterId string, plan Cluster, apiClient *openapiclient.APIClient) error {
	spec, shouldApply, err := expandPlanToGcpSpec(ctx, apiClient, accountId, projectId, clusterId, plan)
	if err != nil || !shouldApply {
		return err
	}
	request := apiClient.BackupReplicationApi.ModifyGcpBackupReplication(ctx, accountId, projectId, clusterId)
	if spec != nil {
		request = request.GcpBackupReplicationSpec(*spec)
	}
	_, response, err := request.Execute()
	if err != nil {
		errMsg := getErrorMessage(response, err)
		return fmt.Errorf("unable to configure GCP backup replication: %s", errMsg)
	}
	return waitForBackupReplicationTask(ctx, apiClient, accountId, projectId, clusterId)
}

func expandPlanToGcpSpec(ctx context.Context, apiClient *openapiclient.APIClient, accountId, projectId, clusterId string, plan Cluster) (*openapiclient.GcpBackupReplicationSpec, bool, error) {
	gcpSpec := plan.BackupReplicationSpec.GCPSpec
	if !boolValue(gcpSpec.Enabled) {
		return &openapiclient.GcpBackupReplicationSpec{}, true, nil
	}

	switch plan.ClusterType.ValueString() {
	case "SYNCHRONOUS":
		config := gcpSpec.SyncClusterSpec
		target := strings.TrimSpace(typesStringValue(config.ReplicationConfig.Target))
		region, regionErr := getPrimaryBackupRegion(ctx, apiClient, accountId, projectId, clusterId)
		if regionErr != nil || region == "" {
			return nil, false, regionErr
		}
		targetSpec := openapiclient.NewGcpBackupReplicationRegionTarget(region)
		t := ""
		if boolValue(gcpSpec.Enabled) {
			t = target
		}
		targetSpec.SetTarget(t)

		return openapiclient.NewGcpBackupReplicationSpec([]openapiclient.GcpBackupReplicationRegionTarget{*targetSpec}), true, nil
	case "GEO_PARTITIONED":
		geo := gcpSpec.GeoPartitionedClusterSpec
		var targets []openapiclient.GcpBackupReplicationRegionTarget
		for _, cfg := range geo.ReplicationConfigs {
			region := strings.TrimSpace(typesStringValue(cfg.DesiredRegion))
			target := strings.TrimSpace(typesStringValue(cfg.Target))
			targetSpec := openapiclient.NewGcpBackupReplicationRegionTarget(region)
			t := ""
			if boolValue(gcpSpec.Enabled) {
				t = target
			}
			targetSpec.SetTarget(t)
			targets = append(targets, *targetSpec)
		}
		return openapiclient.NewGcpBackupReplicationSpec(targets), true, nil
	default:
		return nil, false, fmt.Errorf("backup_replication_spec is not supported for cluster type %s", plan.ClusterType.ValueString())
	}
}

func getPrimaryBackupRegion(ctx context.Context, apiClient *openapiclient.APIClient, accountId, projectId, clusterId string) (string, error) {
	resp, httpResp, err := apiClient.ClusterApi.GetClusterBackupRegions(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		errMsg := getErrorMessage(httpResp, err)
		return "", fmt.Errorf("unable to fetch backup region information: %s", errMsg)
	}
	regions := resp.Data.GetBackupRegions()
	if len(regions) == 0 {
		return "", fmt.Errorf("no backup regions returned for cluster %s", clusterId)
	}
	return regions[0], nil
}

func waitForBackupReplicationTask(ctx context.Context, apiClient *openapiclient.APIClient, accountId, projectId, clusterId string) error {
	retryPolicy := retry.NewConstant(10 * time.Second)
	retryPolicy = retry.WithMaxDuration(3600*time.Second, retryPolicy)
	err := retry.Do(ctx, retryPolicy, func(ctx context.Context) error {
		taskState, readOK, message := getTaskState(accountId, projectId, clusterId, openapiclient.ENTITYTYPEENUM_CLUSTER, openapiclient.TASKTYPEENUM_MODIFY_GCP_BACKUP_REPLICATION, apiClient, ctx)
		if readOK {
			switch taskState {
			case string(openapiclient.TASKACTIONSTATEENUM_SUCCEEDED):
				return nil
			case string(openapiclient.TASKACTIONSTATEENUM_FAILED):
				return ErrFailedTask
			case "TASK_NOT_FOUND":
				return retry.RetryableError(errors.New("backup replication task not found yet"))
			}
		} else {
			return retry.RetryableError(fmt.Errorf("unable to read backup replication task status: %s", message))
		}
		return retry.RetryableError(errors.New("backup replication operation in progress"))
	})
	if err != nil {
		if errors.Is(err, ErrFailedTask) {
			return fmt.Errorf("backup replication operation failed")
		}
		return fmt.Errorf("backup replication operation did not complete: %w", err)
	}
	return nil
}

func readGcpBackupReplicationState(ctx context.Context, apiClient *openapiclient.APIClient, accountId, projectId, clusterId, clusterType string) (*BackupReplicationSpec, error) {
	resp, httpResp, err := apiClient.BackupReplicationApi.GetGcpBackupReplicationConfig(ctx, accountId, projectId, clusterId).Execute()
	if err != nil {
		if httpResp != nil && httpResp.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		errMsg := getErrorMessage(httpResp, err)
		return nil, fmt.Errorf("unable to read GCP backup replication configuration: %s", errMsg)
	}
	return flattenGcpBackupReplicationResponse(resp, clusterType), nil
}

func flattenGcpBackupReplicationResponse(resp openapiclient.GCPBackupReplicationResponse, clusterType string) *BackupReplicationSpec {
	info := resp.Data.Info
	if info.RegionConfigs == nil && info.State == "" {
		return nil
	}
	enabled := info.GetState() != openapiclient.CLUSTERBACKUPREPLICATIONSTATEENUM_DISABLED
	result := &BackupReplicationSpec{
		GCPSpec: &GcpBackupReplicationSpec{
			Enabled: types.BoolValue(enabled),
		},
	}

	regionConfigs := info.GetRegionConfigs()
	if len(regionConfigs) == 0 {
		return result
	}

	var syncConfig *SyncClusterGcpReplicationConfig
	var geoConfigs []GeoClusterGcpReplicationConfig
	var expiryConfigs []GcpBackupReplicationExpiryConfig
	for _, regionConfig := range regionConfigs {
		for _, report := range regionConfig.Reports {
			expiry := extractBackupReplicationExpiry(report)
			if expiry.IsNull() {
				switch clusterType {
				case "GEO_PARTITIONED":
					geoConfigs = append(geoConfigs, buildGeoBackupReplicationConfig(regionConfig.Region, report, expiry))
				case "SYNCHRONOUS":
					if syncConfig == nil {
						syncConfig = buildSyncBackupReplicationConfig(regionConfig.Region, report, expiry)
					}
				default:
					// Unknown cluster type – skip processing but continue collecting expiry configs
				}
			} else {
				expiryConfigs = append(expiryConfigs, buildExpiryBackupReplicationConfig(regionConfig.Region, report, expiry))
			}
		}
	}

	switch clusterType {
	case "SYNCHRONOUS":
		syncSpec := &SyncClusterGcpBackupReplicationSpec{}
		syncSpec.ReplicationConfig = syncConfig
		syncSpec.ConfigsSetForExpiry = expiryConfigs
		result.GCPSpec.SyncClusterSpec = syncSpec
	case "GEO_PARTITIONED":
		geoSpec := &GeoClusterGcpBackupReplicationSpec{
			ReplicationConfigs:  geoConfigs,
			ConfigsSetForExpiry: expiryConfigs,
		}
		result.GCPSpec.GeoPartitionedClusterSpec = geoSpec
	}

	return result
}

func alignGeoGcpReplicationOrder(target *GeoClusterGcpBackupReplicationSpec, reference []GeoClusterGcpReplicationConfig) {
	if target == nil || len(target.ReplicationConfigs) == 0 {
		return
	}

	// If no reference ordering is available, sort alphabetically for determinism.
	if len(reference) == 0 {
		sort.SliceStable(target.ReplicationConfigs, func(i, j int) bool {
			return target.ReplicationConfigs[i].DesiredRegion.ValueString() < target.ReplicationConfigs[j].DesiredRegion.ValueString()
		})
		return
	}

	configByRegion := make(map[string]GeoClusterGcpReplicationConfig, len(target.ReplicationConfigs))
	for _, cfg := range target.ReplicationConfigs {
		configByRegion[cfg.DesiredRegion.ValueString()] = cfg
	}

	var ordered []GeoClusterGcpReplicationConfig
	for _, ref := range reference {
		region := ref.DesiredRegion.ValueString()
		if cfg, ok := configByRegion[region]; ok {
			ordered = append(ordered, cfg)
			delete(configByRegion, region)
		}
	}

	if len(configByRegion) > 0 {
		remaining := make([]GeoClusterGcpReplicationConfig, 0, len(configByRegion))
		for _, cfg := range configByRegion {
			remaining = append(remaining, cfg)
		}
		sort.SliceStable(remaining, func(i, j int) bool {
			return remaining[i].DesiredRegion.ValueString() < remaining[j].DesiredRegion.ValueString()
		})
		ordered = append(ordered, remaining...)
	}

	if len(ordered) > 0 {
		target.ReplicationConfigs = ordered
	}
}

func alignGcpBackupReplicationRegionOrder(cluster *Cluster, reference []GeoClusterGcpReplicationConfig) {
	if cluster == nil ||
		cluster.BackupReplicationSpec == nil ||
		cluster.BackupReplicationSpec.GCPSpec == nil {
		return
	}

	alignGeoGcpReplicationOrder(cluster.BackupReplicationSpec.GCPSpec.GeoPartitionedClusterSpec, reference)
}

func getGeoGcpReplicationRegionReferenceOrder(spec *BackupReplicationSpec) []GeoClusterGcpReplicationConfig {
	if spec == nil || spec.GCPSpec == nil || spec.GCPSpec.GeoPartitionedClusterSpec == nil {
		return nil
	}
	return spec.GCPSpec.GeoPartitionedClusterSpec.ReplicationConfigs
}

func buildSyncBackupReplicationConfig(region string, report openapiclient.GcpBackupReplicationRegionReport, expiry types.String) *SyncClusterGcpReplicationConfig {
	baseCfg := buildBackupReplicationBaseConfig(report, expiry)
	return &SyncClusterGcpReplicationConfig{
		AssignedRegion:                 stringValueOrNull(region),
		Target:                         baseCfg.Target,
		ConfigState:                    baseCfg.ConfigState,
		ID:                             baseCfg.ID,
		NextTransferOperationTime:      baseCfg.NextTransferOperationTime,
		LatestTransferOperationDetails: baseCfg.LatestTransferOperationDetails,
		ExpiryOn:                       baseCfg.ExpiryOn,
	}
}

func buildGeoBackupReplicationConfig(region string, report openapiclient.GcpBackupReplicationRegionReport, expiry types.String) GeoClusterGcpReplicationConfig {
	baseCfg := buildBackupReplicationBaseConfig(report, expiry)
	return GeoClusterGcpReplicationConfig{
		DesiredRegion:                  stringValueOrNull(region),
		Target:                         baseCfg.Target,
		ConfigState:                    baseCfg.ConfigState,
		ID:                             baseCfg.ID,
		NextTransferOperationTime:      baseCfg.NextTransferOperationTime,
		LatestTransferOperationDetails: baseCfg.LatestTransferOperationDetails,
		ExpiryOn:                       baseCfg.ExpiryOn,
	}
}

func buildExpiryBackupReplicationConfig(region string, report openapiclient.GcpBackupReplicationRegionReport, expiry types.String) GcpBackupReplicationExpiryConfig {
	baseCfg := buildBackupReplicationBaseConfig(report, expiry)
	return GcpBackupReplicationExpiryConfig{
		Region:                         stringValueOrNull(region),
		Target:                         baseCfg.Target,
		ConfigState:                    baseCfg.ConfigState,
		ID:                             baseCfg.ID,
		NextTransferOperationTime:      baseCfg.NextTransferOperationTime,
		LatestTransferOperationDetails: baseCfg.LatestTransferOperationDetails,
		ExpiryOn:                       baseCfg.ExpiryOn,
	}
}

func buildBackupReplicationBaseConfig(report openapiclient.GcpBackupReplicationRegionReport, expiry types.String) GcpBackupReplicationBaseConfig {
	return GcpBackupReplicationBaseConfig{
		Target:                         stringValueOrNull(report.Target),
		ConfigState:                    stringValueOrNull(string(report.ConfigState)),
		ID:                             stringValueOrNull(report.Id),
		NextTransferOperationTime:      extractNextTransferOperationTime(report),
		LatestTransferOperationDetails: buildLatestTransferDetails(report),
		ExpiryOn:                       expiry,
	}
}

func extractBackupReplicationExpiry(report openapiclient.GcpBackupReplicationRegionReport) types.String {
	if report.Metadata.IsSet() {
		if meta := report.Metadata.Get(); meta != nil {
			return nullableStringToTypesString(meta.ExpiryOn)
		}
	}
	return types.StringNull()
}

func extractNextTransferOperationTime(report openapiclient.GcpBackupReplicationRegionReport) types.String {
	job, ok := report.GetTransferJobDetailsOk()
	if !ok || job == nil {
		return types.StringNull()
	}
	return timeToTypesString(job.GetNextTransferOperationTime())
}

func buildLatestTransferDetails(report openapiclient.GcpBackupReplicationRegionReport) *GcpBackupReplicationLatestTransferOperationDetails {
	job, ok := report.GetTransferJobDetailsOk()
	if !ok || job == nil {
		return nil
	}
	if details, ok := job.GetLatestTransferOperationDetailsOk(); ok && details != nil {
		return &GcpBackupReplicationLatestTransferOperationDetails{
			StartTime: timeToTypesString(details.StartTime),
			EndTime:   timeToTypesString(details.EndTime),
			Status:    stringValueOrNull(details.Status),
		}
	}
	return nil
}

func nullableStringToTypesString(value openapiclient.NullableString) types.String {
	if !value.IsSet() {
		return types.StringNull()
	}
	val := value.Get()
	if val == nil {
		return types.StringNull()
	}
	return stringValueOrNull(*val)
}

func stringValueOrNull(val string) types.String {
	if strings.TrimSpace(val) == "" {
		return types.StringNull()
	}
	return types.StringValue(val)
}

func timeToTypesString(t time.Time) types.String {
	if t.IsZero() {
		return types.StringNull()
	}
	return types.StringValue(t.UTC().Format(time.RFC3339))
}

func boolValue(val types.Bool) bool {
	return !(val.IsUnknown() || val.IsNull()) && val.ValueBool()
}

func typesStringValue(val types.String) string {
	if val.IsUnknown() || val.IsNull() {
		return ""
	}
	return val.ValueString()
}
