# Root folder
resource "costfluent_folder" "teams" {
  name        = "Teams"
  description = "Team cost allocation"
}

# Nested folder
resource "costfluent_folder" "engineering" {
  name         = "Engineering"
  description  = "Engineering team costs"
  parent_token = costfluent_folder.teams.id
}

resource "costfluent_folder" "marketing" {
  name         = "Marketing"
  parent_token = costfluent_folder.teams.id
}
