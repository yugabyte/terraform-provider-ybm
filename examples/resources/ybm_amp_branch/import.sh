# A branch of a shared cluster is imported by <cluster_name>/<branch_name>
terraform import ybm_amp_branch.dev orders/dev

# A branch of a BYOC cluster is imported by <environment_id>/<cluster_name>/<branch_name>
terraform import ybm_amp_branch.byoc_dev acme-prod/orders/dev
