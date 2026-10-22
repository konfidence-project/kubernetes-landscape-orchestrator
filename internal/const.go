package internal

import konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"

const (
	ControllerName = "konfidence.cloud/kubernetes-landscape-orchestrator"
)

var (
	ArtifactTypeHelm      = konfidencev1alpha1.MustArtifactType("helm.konfidence.cloud")
	ArtifactTypeKustomize = konfidencev1alpha1.MustArtifactType("kustomize.konfidence.cloud")

	SupportedArtifactTypes = konfidencev1alpha1.ArtifactTypeSet{
		ArtifactTypeHelm:      {},
		ArtifactTypeKustomize: {},
	}
)
