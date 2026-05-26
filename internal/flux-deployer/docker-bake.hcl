variable "TAG" {
  default = "dev"
}

variable "REGISTRY" {
  default = "ghcr.io/konfidence-project"
}

group "default" {
  targets = ["landscape-flux-deployer"]
}

target "landscape-flux-deployer" {
  context    = "."
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags       = ["${REGISTRY}/landscape-flux-deployer:${TAG}"]
  
  secret = ["id=gh_token,env=GH_TOKEN"]
}
