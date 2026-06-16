variable "TAG" {
  default = "dev"
}

variable "COMMIT_SHA" {
  default = ""
}

variable "REGISTRY" {
  default = "ghcr.io"
}

group "default" {
  targets = ["kubernetes-landscape-orchestrator"]
}

target "kubernetes-landscape-orchestrator" {
  context    = "."
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags       = concat(
    ["${REGISTRY}/konfidence-project/kubernetes-landscape-orchestrator:${TAG}"],
    COMMIT_SHA != "" ? ["${REGISTRY}/konfidence-project/kubernetes-landscape-orchestrator:${COMMIT_SHA}"] : [],
  )

  secret = ["id=gh_token,env=GH_TOKEN"]
}
