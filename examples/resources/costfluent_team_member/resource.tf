resource "costfluent_team" "platform" {
  name = "Platform"
}

data "costfluent_user" "lena" {
  email = "lena@example.com"
}

# Lena edits in every workspace the platform team can edit
resource "costfluent_team_member" "lena_platform" {
  team_id = costfluent_team.platform.id
  user_id = data.costfluent_user.lena.id
  role    = "editor"
}
