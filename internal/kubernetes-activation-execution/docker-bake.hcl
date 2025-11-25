variable "TAG" {
  default = "dev"
}

variable "REGISTRY" {
  default = "ghcr.io/konfidence-project"
}

group "default" {
  targets = ["landscape-kubernetes-activation-execution-controller"]
}

target "landscape-kubernetes-activation-execution-controller" {
  context    = "."
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags       = ["${REGISTRY}/landscape-kubernetes-activation-execution-controller:${TAG}"]
  
  secret = ["id=gh_token,env=GH_TOKEN"]
}
