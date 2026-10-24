# A cost report is imported by "<workspace ID>:<cost report ID>"; the workspace is required.
#
# An import reads the report back as the API returns it: enums in camel case (last30Days,
# providerName) and the filter as its JSON document. A configuration that writes them another way,
# such as Last30Days or a CQL filter, plans one in-place update after the import that changes
# nothing in the report; write them as the API returns them for an import that plans nothing.
terraform import costfluent_cost_report.monthly_by_service wsp_0000000000000000000001:crp_0000000000000000000001
