# The configuration sets workspace_id: "<workspace ID>:<folder ID>". Import with that workspace,
# because changing a configured workspace_id recreates the folder.
terraform import costfluent_folder.teams wsp_0000000000000000000001:fld_0000000000000000000001

# The configuration leaves workspace_id out: the bare folder ID imports from the provider's
# workspace, which state then keeps.
terraform import costfluent_folder.teams fld_0000000000000000000001
