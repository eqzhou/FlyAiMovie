variable "OCI_DEST" {
  default = "artifacts/flyaimovie-oci.tar"
}

group "default" {
  targets = ["oci"]
}

target "oci" {
  context    = "."
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags       = ["flyaimovie:local"]
  output     = ["type=oci,dest=${OCI_DEST}"]
}
