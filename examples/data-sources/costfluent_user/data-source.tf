# Look a member's ID up by email
data "costfluent_user" "lena" {
  email = "lena@example.com"
}

resource "costfluent_team" "platform" {
  name = "Platform"
}

resource "costfluent_team_member" "lena_platform" {
  team_id = costfluent_team.platform.id
  user_id = data.costfluent_user.lena.id
  role    = "editor"
}
