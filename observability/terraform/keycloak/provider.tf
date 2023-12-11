terraform {
  required_providers {
    keycloak = {
      source  = "mrparkers/keycloak"
      version = "4.3.1"
    }
  }
}

provider "keycloak" {
  client_id     = "admin-cli"
  username      = var.keycloak-username
  password      = var.keycloak-password
  url           = var.keycloak-url
}