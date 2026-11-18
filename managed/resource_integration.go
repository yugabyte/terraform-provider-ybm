/*
 * Copyright © 2022-present Yugabyte, Inc. All Rights Reserved.
 * SPDX-License-Identifier: Apache-2.0
 */
package managed

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	ybmplanmodifier "github.com/yugabyte/terraform-provider-ybm/managed/plan_modifier"
	openapiclient "github.com/yugabyte/yugabytedb-managed-go-client-internal"
)

func (r *resourceIntegration) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Use this resource to create export configurations. Export configurations define parameters for connecting to third party tools, such as Datadog, for exporting cluster metrics and logs.\n" +
			"To use an export configuration, assign it to a cluster using the following resources:\n" +
			"-Export metrics using resource `ybm_associate_metrics_exporter_cluster`\n" +
			"-Export query logs using resource `ybm_db_query_logging`\n" +
			"-Export audit logs using resource `ybm_db_audit_logging`",
		Attributes: r.getSchemaAttributes(),
	}
}

func (r resourceIntegration) getTypeValidator() validator.String {
	validTypes := []string{"DATADOG", "GRAFANA", "SUMOLOGIC", "GOOGLECLOUD", "PROMETHEUS", "VICTORIAMETRICS", "NEWRELIC", "AWS_S3", "TSUGA", "MIMIR"}
	return stringvalidator.OneOf(validTypes...)
}

func (r resourceIntegration) getSchemaAttributes() map[string]schema.Attribute {
	attributes := map[string]schema.Attribute{
		"account_id": schema.StringAttribute{
			Description: "The ID of the account this integration belongs to.",
			Computed:    true,
		},
		"project_id": schema.StringAttribute{
			Description: "The ID of the project this integration belongs to.",
			Computed:    true,
		},
		"config_id": schema.StringAttribute{
			Description: "The ID of the integration.",
			Computed:    true,
		},
		"config_name": schema.StringAttribute{
			Description: "The name of the integration",
			Required:    true,
			PlanModifiers: []planmodifier.String{
				ybmplanmodifier.ImmutableFieldModifier{},
			},
		},
		"type": schema.StringAttribute{
			Description: "Defines different exporter destination types.",
			Required:    true,
			Validators:  []validator.String{r.getTypeValidator()},
			PlanModifiers: []planmodifier.String{
				ybmplanmodifier.ImmutableFieldModifier{},
			},
		},
		"is_valid": schema.BoolAttribute{
			Description: "Signifies whether the integration configuration is valid or not",
			Computed:    true,
		},
		"datadog_spec": schema.SingleNestedAttribute{
			Description: "The specifications of a Datadog integration.",
			Optional:    true,
			PlanModifiers: []planmodifier.Object{
				ybmplanmodifier.ImmutableFieldModifier{},
			},
			Validators: onlyContainsPath("datadog_spec"),
			Attributes: map[string]schema.Attribute{
				"api_key": schema.StringAttribute{
					Description: "Datadog Api Key",
					Required:    true,
					Sensitive:   true,
				},
				"site": schema.StringAttribute{
					Description: "Datadog site.",
					Required:    true,
				}},
		},
		"prometheus_spec": schema.SingleNestedAttribute{
			Description: "The specifications of a Prometheus integration.",
			Optional:    true,
			PlanModifiers: []planmodifier.Object{
				ybmplanmodifier.ImmutableFieldModifier{},
			},
			Validators: onlyContainsPath("prometheus_spec"),
			Attributes: map[string]schema.Attribute{
				"endpoint": schema.StringAttribute{
					Description: "Prometheus OTLP endpoint URL e.g. http://my-prometheus-endpoint/api/v1/otlp",
					Required:    true,
				},
			},
		},
		"victoriametrics_spec": schema.SingleNestedAttribute{
			Description: "The specifications of a VictoriaMetrics integration.",
			Optional:    true,
			PlanModifiers: []planmodifier.Object{
				ybmplanmodifier.ImmutableFieldModifier{},
			},
			Validators: onlyContainsPath("victoriametrics_spec"),
			Attributes: map[string]schema.Attribute{
				"endpoint": schema.StringAttribute{
					Description: "VictoriaMetrics OTLP endpoint URL e.g. http://my-victoria-metrics-endpoint/opentelemetry",
					Required:    true,
				},
			},
		},
		"grafana_spec": schema.SingleNestedAttribute{
			Description: "The specifications of a Grafana integration.",
			Optional:    true,
			PlanModifiers: []planmodifier.Object{
				ybmplanmodifier.ImmutableFieldModifier{},
			},
			Validators: onlyContainsPath("grafana_spec"),
			Attributes: map[string]schema.Attribute{
				"access_policy_token": schema.StringAttribute{
					Description: "Grafana Access Policy Token",
					Required:    true,
					Sensitive:   true,
				},
				"zone": schema.StringAttribute{
					Description: "Grafana Zone.",
					Required:    true,
				},
				"instance_id": schema.StringAttribute{
					Description: "Grafana InstanceID.",
					Required:    true,
				},
				"org_slug": schema.StringAttribute{
					Description: "Grafana OrgSlug.",
					Required:    true,
				},
			},
		},
		"sumologic_spec": schema.SingleNestedAttribute{
			Description: "The specifications of a Sumo Logic integration.",
			Optional:    true,
			PlanModifiers: []planmodifier.Object{
				ybmplanmodifier.ImmutableFieldModifier{},
			},
			Validators: onlyContainsPath("sumologic_spec"),
			Attributes: map[string]schema.Attribute{
				"access_id": schema.StringAttribute{
					Description: "Sumo Logic Access Key ID",
					Required:    true,
					Sensitive:   true,
				},
				"access_key": schema.StringAttribute{
					Description: "Sumo Logic Access Key",
					Required:    true,
					Sensitive:   true,
				}, "installation_token": schema.StringAttribute{
					Description: "A Sumo Logic installation token to export telemetry to Grafana with",
					Required:    true,
					Sensitive:   true,
				},
			},
		},
		"googlecloud_spec": schema.SingleNestedAttribute{
			Description: "The specifications of a Google Cloud integration.",
			Optional:    true,
			PlanModifiers: []planmodifier.Object{
				ybmplanmodifier.ImmutableFieldModifier{},
			},
			Validators: onlyContainsPath("googlecloud_spec"),
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
					Sensitive:   true,
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
		"newrelic_spec": schema.SingleNestedAttribute{
			Description: "The specifications of a New Relic integration.",
			Optional:    true,
			PlanModifiers: []planmodifier.Object{
				ybmplanmodifier.ImmutableFieldModifier{},
			},
			Validators: onlyContainsPath("newrelic_spec"),
			Attributes: map[string]schema.Attribute{
				"endpoint": schema.StringAttribute{
					Description: "New Relic Endpoint URL",
					Required:    true,
				},
				"license_key": schema.StringAttribute{
					Description: "New Relic License Key",
					Required:    true,
					Sensitive:   true,
				},
			},
		},
		"aws_s3_spec": schema.SingleNestedAttribute{
			Description: "The specifications of an AWS S3 integration for exporting database query and audit logs. You can use the same integration with ybm_db_query_logging and ybm_db_audit_logging resources.",
			Optional:    true,
			PlanModifiers: []planmodifier.Object{
				ybmplanmodifier.ImmutableFieldModifier{},
			},
			Validators: onlyContainsPath("aws_s3_spec"),
			Attributes: map[string]schema.Attribute{
				"bucket_name": schema.StringAttribute{
					Description: "The S3 bucket name to export logs to",
					Required:    true,
				},
				"region": schema.StringAttribute{
					Description: "AWS region where the S3 bucket is located",
					Required:    true,
				},
				"access_key_id": schema.StringAttribute{
					Description: "AWS Access Key ID for S3 access",
					Required:    true,
					Sensitive:   true,
				},
				"secret_access_key": schema.StringAttribute{
					Description: "AWS Secret Access Key for S3 access. Use the Access Key ID and Secret Access Key of an IAM user with s3:PutObject permission for the S3 bucket. For more information, refer to [Managing access keys for IAM users](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_access-keys.html) in the AWS documentation.",
					Required:    true,
					Sensitive:   true,
				},
				"path_prefix": schema.StringAttribute{
					Description: "S3 path prefix for organizing objects (Use '/' for root directory). Exported objects use the `{path_prefix}/{cluster-id}/{node-name}/{partition}/` structure, for example `yugabyte-logs/cluster-123/node-1/2024/01/07/14/`.",
					Required:    true,
					Validators: []validator.String{
						stringvalidator.RegexMatches(
							regexp.MustCompile(`^(\/|([a-zA-Z0-9._-]+\/)+)$`),
							"Path prefix should match the regex pattern ^(\\/|([a-zA-Z0-9._-]+\\/)+)$",
						),
					},
				},
				"file_prefix": schema.StringAttribute{
					Description: "Prefix for exported file names",
					Optional:    true,
					Computed:    true,
				},
				"partition_strategy": schema.StringAttribute{
					Description: "Time-based partitioning: 'minute' or 'hour' (default: hour)",
					Optional:    true,
					Computed:    true,
					Validators:  []validator.String{stringvalidator.OneOf("minute", "hour")},
				},
			},
		},
		"tsuga_spec": schema.SingleNestedAttribute{
			Description: "The specifications of a Tsuga integration. A Tsuga integration can be used to export metrics, database query logs and database audit logs.",
			Optional:    true,
			PlanModifiers: []planmodifier.Object{
				ybmplanmodifier.ImmutableFieldModifier{},
			},
			Validators: onlyContainsPath("tsuga_spec"),
			Attributes: map[string]schema.Attribute{
				"endpoint": schema.StringAttribute{
					Description: "Tsuga OTLP endpoint URL e.g. https://intake.my-org.tsuga.com/api/v1/otlp. Do not include the /v1/metrics suffix, it is appended automatically.",
					Required:    true,
				},
				"api_token": schema.StringAttribute{
					Description: "Tsuga bearer token used to export telemetry to Tsuga.",
					Required:    true,
					Sensitive:   true,
					Validators:  []validator.String{stringvalidator.LengthBetween(6, 600)},
				},
			},
		},
		"mimir_spec": schema.SingleNestedAttribute{
			Description: "The specifications of a Mimir integration. A Mimir integration can be used to export metrics only.",
			Optional:    true,
			PlanModifiers: []planmodifier.Object{
				ybmplanmodifier.ImmutableFieldModifier{},
			},
			Validators: onlyContainsPath("mimir_spec"),
			Attributes: map[string]schema.Attribute{
				"endpoint": schema.StringAttribute{
					Description: "Mimir OTLP endpoint URL e.g. https://mimir.my-company.com/otlp. Do not include the /v1/metrics suffix, it is appended automatically.",
					Required:    true,
				},
				"api_token": schema.StringAttribute{
					Description: "The full Authorization header value used to export metrics to Mimir. Must start with \"Bearer \" or \"Basic \".",
					Required:    true,
					Sensitive:   true,
					Validators: []validator.String{
						stringvalidator.LengthBetween(6, 600),
						stringvalidator.RegexMatches(
							regexp.MustCompile(`^(Bearer|Basic) `),
							"value must start with \"Bearer \" or \"Basic \"",
						),
					},
				},
				"org_id": schema.StringAttribute{
					Description: "Mimir tenant organization ID, sent as the X-Scope-OrgID header.",
					Optional:    true,
				},
			},
		},
	}

	return attributes
}

func onlyContainsPath(requiredPath string) []validator.Object {
	allPaths := []string{"datadog_spec", "grafana_spec", "sumologic_spec", "googlecloud_spec", "prometheus_spec", "victoriametrics_spec", "newrelic_spec", "aws_s3_spec", "tsuga_spec", "mimir_spec"}

	var validators []validator.Object

	for _, specPath := range allPaths {
		if specPath != requiredPath {
			validators = append(validators, objectvalidator.ConflictsWith(path.MatchRoot(specPath)))
		}
	}

	return validators
}

func newResourceIntegration() resource.Resource {
	return &resourceIntegration{}
}

func (r *resourceIntegration) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration"
}

func (r *resourceIntegration) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	r.p = configuredProvider(req.ProviderData)
}

type resourceIntegration struct {
	p provider
}

func getIntegrationPlan(ctx context.Context, plan tfsdk.Plan, tp *TelemetryProvider) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(plan.GetAttribute(ctx, path.Root("config_name"), &tp.ConfigName)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("type"), &tp.Type)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("datadog_spec"), &tp.DataDogSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("prometheus_spec"), &tp.PrometheusSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("victoriametrics_spec"), &tp.VictoriaMetricsSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("grafana_spec"), &tp.GrafanaSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("sumologic_spec"), &tp.SumoLogicSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("googlecloud_spec"), &tp.GoogleCloudSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("newrelic_spec"), &tp.NewRelicSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("aws_s3_spec"), &tp.AwsS3Spec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("tsuga_spec"), &tp.TsugaSpec)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("mimir_spec"), &tp.MimirSpec)...)

	return diags
}

func getIDsFromIntegrationState(ctx context.Context, state tfsdk.State, tp *TelemetryProvider) {
	state.GetAttribute(ctx, path.Root("account_id"), &tp.AccountID)
	state.GetAttribute(ctx, path.Root("project_id"), &tp.ProjectID)
	state.GetAttribute(ctx, path.Root("config_id"), &tp.ConfigID)
	state.GetAttribute(ctx, path.Root("type"), &tp.Type)
	switch tp.Type.ValueString() {
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG):
		state.GetAttribute(ctx, path.Root("datadog_spec"), &tp.DataDogSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_PROMETHEUS):
		state.GetAttribute(ctx, path.Root("prometheus_spec"), &tp.PrometheusSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_VICTORIAMETRICS):
		state.GetAttribute(ctx, path.Root("victoriametrics_spec"), &tp.VictoriaMetricsSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA):
		state.GetAttribute(ctx, path.Root("grafana_spec"), &tp.GrafanaSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC):
		state.GetAttribute(ctx, path.Root("sumologic_spec"), &tp.SumoLogicSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_GOOGLECLOUD):
		state.GetAttribute(ctx, path.Root("googlecloud_spec"), &tp.GoogleCloudSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_NEWRELIC):
		state.GetAttribute(ctx, path.Root("newrelic_spec"), &tp.NewRelicSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_AWS_S3):
		state.GetAttribute(ctx, path.Root("aws_s3_spec"), &tp.AwsS3Spec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_TSUGA):
		state.GetAttribute(ctx, path.Root("tsuga_spec"), &tp.TsugaSpec)
	case string(openapiclient.TELEMETRYPROVIDERTYPEENUM_MIMIR):
		state.GetAttribute(ctx, path.Root("mimir_spec"), &tp.MimirSpec)
	}
}

func (r resourceIntegration) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {

	if !r.p.configured {
		resp.Diagnostics.AddError(
			"Provider not configured",
			"The provider wasn't configured before being applied, likely because it depends on an unknown value from another resource.",
		)
		return
	}

	var plan TelemetryProvider
	resp.Diagnostics.Append(getIntegrationPlan(ctx, req.Plan, &plan)...)
	if resp.Diagnostics.HasError() {
		tflog.Debug(ctx, "Error while getting the plan for the integration")
		return
	}

	sinkType := plan.Type.ValueString()
	telemetrySinkTypeEnum, err := openapiclient.NewTelemetryProviderTypeEnumFromValue(strings.ToUpper(sinkType))
	if err != nil {
		resp.Diagnostics.AddError(GetApiErrorDetails(err), "")
		return
	}

	apiClient := r.p.client

	accountId, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get the project ID", message)
		return
	}

	configName := plan.ConfigName.ValueString()

	telemetryProviderSpec := openapiclient.NewTelemetryProviderSpec(configName, *telemetrySinkTypeEnum)

	switch *telemetrySinkTypeEnum {
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG:
		if plan.DataDogSpec == nil {
			resp.Diagnostics.AddError(
				"datadog_spec is required for type DATADOG",
				"datadog_spec is required when telemetry sink is DATADOG. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetDatadogSpec(*openapiclient.NewDatadogTelemetryProviderSpec(plan.DataDogSpec.ApiKey.ValueString(), plan.DataDogSpec.Site.ValueString()))
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_PROMETHEUS:
		if plan.PrometheusSpec == nil {
			resp.Diagnostics.AddError(
				"prometheus_spec is required for type PROMETHEUS",
				"prometheus_spec is required when telemetry sink is PROMETHEUS. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetPrometheusSpec(*openapiclient.NewPrometheusTelemetryProviderSpec(plan.PrometheusSpec.Endpoint.ValueString()))
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_VICTORIAMETRICS:
		if plan.VictoriaMetricsSpec == nil {
			resp.Diagnostics.AddError(
				"victoriametrics_spec is required for type VICTORIAMETRICS",
				"victoriametrics_spec is required when telemetry sink is VICTORIAMETRICS. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetVictoriametricsSpec(*openapiclient.NewVictoriaMetricsTelemetryProviderSpec(plan.VictoriaMetricsSpec.Endpoint.ValueString()))
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA:
		if plan.GrafanaSpec == nil {
			resp.Diagnostics.AddError(
				"grafana_spec is required for type GRAFANA",
				"grafana_spec is required when telemetry sink is GRAFANA. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetGrafanaSpec(*openapiclient.NewGrafanaTelemetryProviderSpec(plan.GrafanaSpec.AccessTokenPolicy.ValueString(), plan.GrafanaSpec.Zone.ValueString(), plan.GrafanaSpec.InstanceId.ValueString(), plan.GrafanaSpec.OrgSlug.ValueString()))

	case openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC:
		if plan.SumoLogicSpec == nil {
			resp.Diagnostics.AddError(
				"sumologic_spec is required for type SUMOLOGIC",
				"sumologic_spec is required when telemetry sink is SUMOLOGIC. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetSumologicSpec(*openapiclient.NewSumologicTelemetryProviderSpec(plan.SumoLogicSpec.InstallationToken.ValueString(), plan.SumoLogicSpec.AccessId.ValueString(), plan.SumoLogicSpec.AccessKey.ValueString()))

	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GOOGLECLOUD:
		if plan.GoogleCloudSpec == nil {
			resp.Diagnostics.AddError(
				"googlecloud_spec is required for type GOOGLECLOUD",
				"googlecloud_spec is required when telemetry sink is GOOGLECLOUD. Please include this field in the resource",
			)
			return
		}
		gcpServiceAccountPlan := plan.GoogleCloudSpec
		googleCloudSpec := *openapiclient.NewGCPServiceAccount(gcpServiceAccountPlan.Type.ValueString(), gcpServiceAccountPlan.ProjectId.ValueString(), gcpServiceAccountPlan.PrivateKey.ValueString(), gcpServiceAccountPlan.PrivateKeyId.ValueString(), gcpServiceAccountPlan.ClientEmail.ValueString(), gcpServiceAccountPlan.ClientId.ValueString(), gcpServiceAccountPlan.AuthUri.ValueString(), gcpServiceAccountPlan.TokenUri.ValueString(), gcpServiceAccountPlan.AuthProviderX509CertUrl.ValueString(), gcpServiceAccountPlan.ClientX509CertUrl.ValueString())
		if !gcpServiceAccountPlan.UniverseDomain.IsNull() && !gcpServiceAccountPlan.UniverseDomain.IsUnknown() && gcpServiceAccountPlan.UniverseDomain.ValueString() != "" {
			googleCloudSpec.SetUniverseDomain(gcpServiceAccountPlan.UniverseDomain.ValueString())
		}
		telemetryProviderSpec.SetGooglecloudSpec(googleCloudSpec)
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_NEWRELIC:
		if plan.NewRelicSpec == nil {
			resp.Diagnostics.AddError(
				"newrelic_spec is required for type NEWRELIC",
				"newrelic_spec is required when telemetry sink is NEWRELIC. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetNewrelicSpec(*openapiclient.NewNewrelicTelemetryProviderSpec(plan.NewRelicSpec.LicenseKey.ValueString(), plan.NewRelicSpec.Endpoint.ValueString()))
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_AWS_S3:
		if plan.AwsS3Spec == nil {
			resp.Diagnostics.AddError(
				"aws_s3_spec is required for type AWS_S3",
				"aws_s3_spec is required when telemetry sink is AWS_S3. Please include this field in the resource",
			)
			return
		}
		awsS3SpecPlan := plan.AwsS3Spec
		s3TelemetrySpec := *openapiclient.NewS3TelemetryProviderSpec(awsS3SpecPlan.BucketName.ValueString(), awsS3SpecPlan.Region.ValueString(), awsS3SpecPlan.AccessKeyId.ValueString(), awsS3SpecPlan.SecretAccessKey.ValueString(), awsS3SpecPlan.PathPrefix.ValueString())

		// Set optional fields if provided
		if !awsS3SpecPlan.FilePrefix.IsNull() && !awsS3SpecPlan.FilePrefix.IsUnknown() && awsS3SpecPlan.FilePrefix.ValueString() != "" {
			s3TelemetrySpec.SetFilePrefix(awsS3SpecPlan.FilePrefix.ValueString())
		}
		if !awsS3SpecPlan.PartitionStrategy.IsNull() && !awsS3SpecPlan.PartitionStrategy.IsUnknown() && awsS3SpecPlan.PartitionStrategy.ValueString() != "" {
			s3TelemetrySpec.SetPartitionStrategy(awsS3SpecPlan.PartitionStrategy.ValueString())
		}
		telemetryProviderSpec.SetAwsS3Spec(s3TelemetrySpec)
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_TSUGA:
		if plan.TsugaSpec == nil {
			resp.Diagnostics.AddError(
				"tsuga_spec is required for type TSUGA",
				"tsuga_spec is required when telemetry sink is TSUGA. Please include this field in the resource",
			)
			return
		}
		telemetryProviderSpec.SetTsugaSpec(*openapiclient.NewTsugaTelemetryProviderSpec(plan.TsugaSpec.ApiToken.ValueString(), plan.TsugaSpec.Endpoint.ValueString()))
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_MIMIR:
		if plan.MimirSpec == nil {
			resp.Diagnostics.AddError(
				"mimir_spec is required for type MIMIR",
				"mimir_spec is required when telemetry sink is MIMIR. Please include this field in the resource",
			)
			return
		}
		mimirSpec := *openapiclient.NewMimirTelemetryProviderSpec(plan.MimirSpec.ApiToken.ValueString(), plan.MimirSpec.Endpoint.ValueString())
		if !plan.MimirSpec.OrgId.IsNull() && !plan.MimirSpec.OrgId.IsUnknown() && plan.MimirSpec.OrgId.ValueString() != "" {
			mimirSpec.SetOrgId(plan.MimirSpec.OrgId.ValueString())
		}
		telemetryProviderSpec.SetMimirSpec(mimirSpec)
	default:
		//We should never go there normally
		resp.Diagnostics.AddError(
			"Only DATADOG, GRAFANA, SUMOLOGIC, GOOGLECLOUD, PROMETHEUS, VICTORIAMETRICS, NEWRELIC, AWS_S3, TSUGA and MIMIR are currently supported as integrations",
			"",
		)
		return
	}

	CreateResp, _, err := apiClient.TelemetryProviderApi.CreateTelemetryProvider(ctx, accountId, projectId).TelemetryProviderSpec(*telemetryProviderSpec).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to create integration", GetApiErrorDetails(err))
		return
	}

	telemetryProvider, readOK, message := resourceTelemetryProviderRead(accountId, projectId, CreateResp.GetData().Info.Id, apiClient, plan)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the integration", message)
		return
	}

	diags := resp.State.Set(ctx, &telemetryProvider)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r resourceIntegration) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TelemetryProvider

	getIDsFromIntegrationState(ctx, req.State, &state)
	configID := state.ConfigID.ValueString()

	apiClient := r.p.client

	accountId, getAccountOK, message := getAccountId(ctx, apiClient)
	if !getAccountOK {
		resp.Diagnostics.AddError("Unable to get account ID", message)
		return
	}

	projectId, getProjectOK, message := getProjectId(ctx, apiClient, accountId)
	if !getProjectOK {
		resp.Diagnostics.AddError("Unable to get project ID ", message)
		return
	}

	config, readOK, message := resourceTelemetryProviderRead(accountId, projectId, configID, apiClient, state)
	if !readOK {
		resp.Diagnostics.AddError("Unable to read the state of the integration", message)
		return
	}

	diags := resp.State.Set(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r resourceIntegration) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Unsupported Operation",
		"This resource does not support updates. Please destroy and recreate the resource if changes are needed.",
	)
}

func resourceTelemetryProviderRead(accountId string, projectId string, configID string, apiClient *openapiclient.APIClient, userProvidedTpDetails TelemetryProvider) (tp TelemetryProvider, readOK bool, errorMessage string) {
	// userProvidedTpDetails: Telemetry provider details from the state or plan used to set the credentials which are masked when obtained from the API

	configData, err := GetTelemetryProviderById(accountId, projectId, configID, apiClient)
	if err != nil {
		return tp, false, GetApiErrorDetails(err)
	}

	configSpec := configData.GetSpec()
	configInfo := configData.GetInfo()

	tp.AccountID = types.StringValue(accountId)
	tp.ProjectID = types.StringValue(projectId)
	tp.ConfigName = types.StringValue(configSpec.Name)
	tp.ConfigID = types.StringValue(configInfo.Id)
	tp.Type = types.StringValue(string(configSpec.Type))
	tp.IsValid = types.BoolValue(*configInfo.IsValid.Get())

	// API returns masked credentials. We use the credential details provided by the user in the plan or the existing state
	switch configData.GetSpec().Type {
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_DATADOG:
		tp.DataDogSpec = &DataDogSpec{
			ApiKey: userProvidedTpDetails.DataDogSpec.ApiKey,
			Site:   types.StringValue(configSpec.DatadogSpec.Get().Site),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_PROMETHEUS:
		tp.PrometheusSpec = &PrometheusSpec{
			Endpoint: types.StringValue(userProvidedTpDetails.PrometheusSpec.Endpoint.ValueString()),
		}

	case openapiclient.TELEMETRYPROVIDERTYPEENUM_VICTORIAMETRICS:
		tp.VictoriaMetricsSpec = &VictoriaMetricsSpec{
			Endpoint: types.StringValue(configSpec.VictoriametricsSpec.Get().Endpoint),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GRAFANA:
		grafanaSpec := configSpec.GetGrafanaSpec()
		tp.GrafanaSpec = &GrafanaSpec{
			AccessTokenPolicy: userProvidedTpDetails.GrafanaSpec.AccessTokenPolicy,
			Zone:              types.StringValue(grafanaSpec.Zone),
			InstanceId:        types.StringValue(grafanaSpec.InstanceId),
			OrgSlug:           types.StringValue(grafanaSpec.OrgSlug),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_SUMOLOGIC:
		tp.SumoLogicSpec = &SumoLogicSpec{
			AccessKey:         userProvidedTpDetails.SumoLogicSpec.AccessKey,
			AccessId:          userProvidedTpDetails.SumoLogicSpec.AccessId,
			InstallationToken: userProvidedTpDetails.SumoLogicSpec.InstallationToken,
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_GOOGLECLOUD:
		googlecloudSpec := configSpec.GetGooglecloudSpec()
		tp.GoogleCloudSpec = &GCPServiceAccount{
			Type:                    types.StringValue(googlecloudSpec.Type),
			ProjectId:               types.StringValue(googlecloudSpec.ProjectId),
			PrivateKeyId:            types.StringValue(googlecloudSpec.PrivateKeyId),
			PrivateKey:              userProvidedTpDetails.GoogleCloudSpec.PrivateKey,
			ClientEmail:             types.StringValue(googlecloudSpec.ClientEmail),
			ClientId:                types.StringValue(googlecloudSpec.ClientId),
			AuthUri:                 types.StringValue(googlecloudSpec.AuthUri),
			TokenUri:                types.StringValue(googlecloudSpec.TokenUri),
			AuthProviderX509CertUrl: types.StringValue(googlecloudSpec.AuthProviderX509CertUrl),
			ClientX509CertUrl:       types.StringValue(googlecloudSpec.ClientX509CertUrl),
			UniverseDomain:          types.StringValue(googlecloudSpec.GetUniverseDomain()),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_NEWRELIC:
		tp.NewRelicSpec = &NewRelicSpec{
			Endpoint:   types.StringValue(configSpec.NewrelicSpec.Get().Endpoint),
			LicenseKey: userProvidedTpDetails.NewRelicSpec.LicenseKey,
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_AWS_S3:
		s3Spec := configSpec.GetAwsS3Spec()
		tp.AwsS3Spec = &AwsS3Spec{
			BucketName:      types.StringValue(s3Spec.BucketName),
			Region:          types.StringValue(s3Spec.Region),
			AccessKeyId:     userProvidedTpDetails.AwsS3Spec.AccessKeyId,     // Use user-provided value (API returns masked)
			SecretAccessKey: userProvidedTpDetails.AwsS3Spec.SecretAccessKey, // Use user-provided value (API returns masked)
			PathPrefix:      types.StringValue(s3Spec.GetPathPrefix()),
			// Optional fields; "" when the API response does not have them.
			FilePrefix:        types.StringValue(s3Spec.GetFilePrefix()),
			PartitionStrategy: types.StringValue(s3Spec.GetPartitionStrategy()),
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_TSUGA:
		// The API masks api_token and normalizes the endpoint (e.g. strips trailing slashes),
		// so keep the user-provided values to avoid inconsistent results / perpetual diffs.
		if userProvidedTpDetails.TsugaSpec != nil {
			tp.TsugaSpec = &TsugaSpec{
				Endpoint: userProvidedTpDetails.TsugaSpec.Endpoint,
				ApiToken: userProvidedTpDetails.TsugaSpec.ApiToken,
			}
		} else {
			tsugaSpec := configSpec.GetTsugaSpec()
			tp.TsugaSpec = &TsugaSpec{
				Endpoint: types.StringValue(tsugaSpec.Endpoint),
				ApiToken: types.StringValue(tsugaSpec.ApiToken),
			}
		}
	case openapiclient.TELEMETRYPROVIDERTYPEENUM_MIMIR:
		// Same as Tsuga: keep user-provided values; org_id stays null when not set by the user.
		if userProvidedTpDetails.MimirSpec != nil {
			tp.MimirSpec = &MimirSpec{
				Endpoint: userProvidedTpDetails.MimirSpec.Endpoint,
				ApiToken: userProvidedTpDetails.MimirSpec.ApiToken,
				OrgId:    userProvidedTpDetails.MimirSpec.OrgId,
			}
		} else {
			mimirSpec := configSpec.GetMimirSpec()
			tp.MimirSpec = &MimirSpec{
				Endpoint: types.StringValue(mimirSpec.Endpoint),
				ApiToken: types.StringValue(mimirSpec.ApiToken),
				OrgId:    types.StringNull(),
			}
			if mimirSpec.GetOrgId() != "" {
				tp.MimirSpec.OrgId = types.StringValue(mimirSpec.GetOrgId())
			}
		}
	}

	return tp, true, ""
}

func (r resourceIntegration) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	apiClient := r.p.client
	var state TelemetryProvider
	getIDsFromIntegrationState(ctx, req.State, &state)
	accountId := state.AccountID.ValueString()
	projectId := state.ProjectID.ValueString()
	configId := state.ConfigID.ValueString()

	_, err := apiClient.TelemetryProviderApi.DeleteTelemetryProvider(ctx, accountId, projectId, configId).Execute()
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete the integration", GetApiErrorDetails(err))
		return
	}

	resp.State.RemoveResource(ctx)
}

func GetTelemetryProviderById(accountId string, projectId string, configID string, apiClient *openapiclient.APIClient) (*openapiclient.TelemetryProviderData, error) {
	resp, _, err := apiClient.TelemetryProviderApi.ListTelemetryProviders(context.Background(), accountId, projectId).Execute()

	if err != nil {
		return nil, err
	}

	for _, telemetryProvider := range resp.Data {
		if telemetryProvider.GetInfo().Id == configID {
			return &telemetryProvider, nil
		}
	}

	return nil, fmt.Errorf("could not find integration with id: %s", configID)
}

// Import API Key
func (r resourceIntegration) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Save the import identifier in the id attribute
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
