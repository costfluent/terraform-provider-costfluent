# Top-level folder
resource "costfluent_folder" "teams" {
  title = "Teams"
}

# Nested folders
resource "costfluent_folder" "engineering" {
  title     = "Engineering"
  parent_id = costfluent_folder.teams.id
}

resource "costfluent_folder" "marketing" {
  title     = "Marketing"
  parent_id = costfluent_folder.teams.id
}
