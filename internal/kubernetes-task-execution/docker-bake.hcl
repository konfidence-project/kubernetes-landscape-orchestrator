variable "TAG" {
  default = "dev"
}

variable "REGISTRY" {
  default = "ghcr.io/konfidence-project"
}

group "default" {
  targets = ["landscape-kubernetes-task-execution-controller"]
}

target "landscape-kubernetes-task-execution-controller" {
  context    = "."
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags       = ["${REGISTRY}/landscape-kubernetes-task-execution-controller:${TAG}"]
  
  secret = ["id=gh_token,env=GH_TOKEN"]
}
