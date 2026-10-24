# The configuration sets workspace_id: "<workspace ID>:<dashboard ID>". Import with that
# workspace, because changing a configured workspace_id recreates the dashboard.
terraform import costfluent_dashboard.executive wsp_0000000000000000000001:dsh_0000000000000000000001

# The configuration leaves workspace_id out: the bare dashboard ID imports from the provider's
# workspace, which state then keeps.
terraform import costfluent_dashboard.executive dsh_0000000000000000000001
