# The configuration sets workspace_id: "<workspace ID>:<savings view ID>". Import with that
# workspace, because changing a configured workspace_id recreates the savings view.
terraform import costfluent_savings_view.aws_in_progress wsp_0000000000000000000001:svw_0000000000000000000001

# The configuration leaves workspace_id out: the bare savings view ID imports from the provider's
# workspace, which state then keeps.
terraform import costfluent_savings_view.rightsizing svw_0000000000000000000001
