# An import reads a rule without accounts with account_ids unset, so a rule written with
# account_ids = [] plans one in-place update after the import that changes nothing in the policy.

# The import ID is the workspace ID: a workspace has one tag policy.
terraform import costfluent_tag_policy.production wsp_0000000000000000000001
