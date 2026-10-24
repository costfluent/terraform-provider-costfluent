# The configuration sets workspace_id: "<workspace ID>:<business metric ID>". Import with that
# workspace, because changing a configured workspace_id recreates the business metric.
terraform import costfluent_business_metric.customers wsp_0000000000000000000001:bmt_0000000000000000000001

# The configuration leaves workspace_id out: the bare business metric ID imports from the
# provider's workspace, which state then keeps.
terraform import costfluent_business_metric.customers bmt_0000000000000000000001
