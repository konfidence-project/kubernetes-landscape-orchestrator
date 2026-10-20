package internal

import konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"

const (
	ControllerName = "konfidence.cloud/kubernetes-landscape-orchestrator"
)

var (
	DeploymentClassHelm      = konfidencev1alpha1.MustArtifactType("helm.konfidence.cloud")
	DeploymentClassKustomize = konfidencev1alpha1.MustArtifactType("kustomize.konfidence.cloud")

	SupportedArtifactTypes = konfidencev1alpha1.ArtifactTypeSet{
		DeploymentClassHelm:      {},
		DeploymentClassKustomize: {},
	}
)
