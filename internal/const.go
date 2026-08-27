package internal

const (
	ControllerName = "konfidence.cloud/kubernetes-landscape-orchestrator"

	DeploymentClassHelm      = "helm.konfidence.cloud"
	DeploymentClassKustomize = "kustomize.konfidence.cloud"
)

var KnownClasses = map[string]struct{}{
	DeploymentClassHelm:      {},
	DeploymentClassKustomize: {},
}
