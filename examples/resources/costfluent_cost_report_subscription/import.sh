# The configuration sets workspace_id: "<workspace ID>:<report notification ID>". Import with that
# workspace, because changing a configured workspace_id recreates the report notification.
terraform import costfluent_cost_report_subscription.weekly wsp_0000000000000000000001:crs_0000000000000000000001

# The configuration leaves workspace_id out: the bare ID imports from the provider's workspace,
# which state then keeps.
terraform import costfluent_cost_report_subscription.weekly crs_0000000000000000000001
