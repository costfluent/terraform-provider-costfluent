# The configuration sets workspace_id: "<workspace ID>:<cost alert ID>". Import with that
# workspace, because changing a configured workspace_id recreates the cost alert.
terraform import costfluent_cost_alert.high_spend wsp_0000000000000000000001:cal_0000000000000000000001

# The configuration leaves workspace_id out: the bare cost alert ID imports from the provider's
# workspace, which state then keeps.
terraform import costfluent_cost_alert.high_spend cal_0000000000000000000001
