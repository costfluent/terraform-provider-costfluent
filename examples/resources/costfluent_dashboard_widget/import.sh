# A widget is addressed through its dashboard.

# The configuration sets workspace_id: "<workspace ID>:<dashboard ID>:<widget ID>". Import with
# that workspace, because changing a configured workspace_id recreates the widget.
terraform import costfluent_dashboard_widget.spend wsp_0000000000000000000001:dsh_0000000000000000000001:wdg_0000000000000000000001

# The configuration leaves workspace_id out: "<dashboard ID>:<widget ID>" imports from the
# provider's workspace, which state then keeps.
terraform import costfluent_dashboard_widget.spend dsh_0000000000000000000001:wdg_0000000000000000000001
