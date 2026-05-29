variable "TAG" {
  default = "dev"
}

variable "REGISTRY" {
  default = "ghcr.io/konfidence-project"
}

group "default" {
  targets = ["kubernetes-landscape-orchestrator"]
}

target "kubernetes-landscape-orchestrator" {
  context    = "."
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags       = ["${REGISTRY}/kubernetes-landscape-orchestrator:${TAG}"]

  secret = ["id=gh_token,env=GH_TOKEN"]
}
