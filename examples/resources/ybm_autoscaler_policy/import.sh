# Autoscaler policy can be imported using cluster_id,region,type.
# type must be PRIMARY or READ_REPLICA.

# Example:
terraform import ybm_autoscaler_policy.my_autoscaler_policy cluster_id,us-west1,PRIMARY
