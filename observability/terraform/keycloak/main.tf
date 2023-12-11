#data "keycloak_realm" "master" {
#  realm = "master"
#}
#
#resource "keycloak_realm_events" "master_events" {
#  realm_id = data.keycloak_realm.master.id
#  events_listeners = [
#    "jboss-logging",
#    "metrics-listener",
#  ]
#}

resource "keycloak_realm" "consumer" {
  realm = "consumer"
  enabled = "true"
  attributes      = {
    frontendUrl = "http://localhost:8080/auth"
  }
}

#resource "keycloak_realm_events" "web_events" {
#  realm_id = keycloak_realm.consumer.id
#  events_listeners = [
#    "jboss-logging",
#    "metrics-listener",
#  ]
#}

resource "keycloak_openid_client" "web" {
  realm_id  = keycloak_realm.consumer.id
  client_id = "web"
  enabled = true
  access_type = "PUBLIC"
  login_theme = "keycloak"
  valid_redirect_uris = [
    "*"
  ]
  standard_flow_enabled = true
  direct_access_grants_enabled  = true
  web_origins = [
    "*"
  ]
}

resource "keycloak_user" "regular-demo-user" {
  realm_id   = keycloak_realm.consumer.id
  username   = "regular-demo-user"
  enabled    = true

  first_name = "Taro"
  last_name  = "Test"

  initial_password {
    value     = "password"
    temporary = false
  }
}

resource "keycloak_user" "extend-demo-user" {
  realm_id   = keycloak_realm.consumer.id
  username   = "extend-demo-user"
  enabled    = true

  first_name = "Taro"
  last_name  = "Ex"

  initial_password {
    value     = "password"
    temporary = false
  }
}

resource "keycloak_role" "reqular_role" {
  realm_id    = keycloak_realm.consumer.id
  client_id   = keycloak_openid_client.web.id
  name        = "regular"
  description = "For Regular Client Role."
}

resource "keycloak_role" "extended_role" {
  realm_id    = keycloak_realm.consumer.id
  client_id   = keycloak_openid_client.web.id
  name        = "extended"
  description = "For Extended Client Role."
}


resource "keycloak_user_roles" "regular_user" {
  realm_id = keycloak_realm.consumer.id
  user_id  = keycloak_user.regular-demo-user.id

  role_ids = [
    keycloak_role.reqular_role.id
  ]
}

resource "keycloak_user_roles" "extended_user" {
  realm_id = keycloak_realm.consumer.id
  user_id  = keycloak_user.extend-demo-user.id

  role_ids = [
    keycloak_role.extended_role.id
  ]
}