# An endpoint of a shared cluster is imported by <cluster_name>/<branch_name>/<endpoint_name>
terraform import ybm_amp_endpoint.replica orders/main/replica

# An endpoint of a BYOC cluster is imported by <environment_id>/<storage_pool>/<cluster_name>/<branch_name>/<endpoint_name>
terraform import ybm_amp_endpoint.byoc_replica acme-prod/payments/orders/main/replica
