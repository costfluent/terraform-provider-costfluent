# An import reads each rule's filter back as its JSON document, so a rule written as CQL plans one
# in-place update after the import that changes nothing in the tag.

# The configuration sets workspace_id: "<workspace ID>:<virtual tag ID>". Import with that
# workspace, because changing a configured workspace_id recreates the virtual tag.
terraform import costfluent_virtual_tag.team wsp_0000000000000000000001:vtag_0000000000000000000001

# The configuration leaves workspace_id out: the bare virtual tag ID imports from the provider's
# workspace, which state then keeps.
terraform import costfluent_virtual_tag.team vtag_0000000000000000000001
