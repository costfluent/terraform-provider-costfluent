# Import with the form that matches the configuration's workspace_id, because changing it recreates
# the budget. The API does not return a budget's workspace, so only the import ID can set it.

# The configuration sets workspace_id: "<workspace ID>:<budget ID>". The import is refused when the
# budget is not in that workspace.
terraform import costfluent_budget.monthly wsp_0000000000000000000001:bud_0000000000000000000001

# The configuration leaves workspace_id to the provider's workspace: the bare budget ID.
terraform import costfluent_budget.monthly bud_0000000000000000000001
