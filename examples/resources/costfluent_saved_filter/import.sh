# The configuration sets workspace_id: "<workspace ID>:<saved filter ID>". Import with that
# workspace, because changing a configured workspace_id recreates the saved filter.
terraform import costfluent_saved_filter.production wsp_0000000000000000000001:flt_0000000000000000000001

# The configuration leaves workspace_id out: the bare saved filter ID imports from the provider's
# workspace, which state then keeps.
terraform import costfluent_saved_filter.production flt_0000000000000000000001
