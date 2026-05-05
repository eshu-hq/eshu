terraform {
  required_providers {
    oci = {
      source  = "oracle/oci"
      version = "~> 6.0"
    }
  }
}

provider "oci" {
  tenancy_ocid = "placeholder"
  user_ocid    = "placeholder"
  fingerprint  = "placeholder"
  private_key  = "placeholder"
  region       = "us-ashburn-1"
}
