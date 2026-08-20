package reconciler

import (
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	pkgctrl "github.com/konfidence-project/konfidence/pkg/controller"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd/utils"
)

const (
	conditionTypeReady               = "Ready"
	maxKustomizationNameSuffixLength = 36
)

// buildKustomizationNameSuffix builds a NameSuffix of the form -<version>-<hash>, falling
// back to -<hash> when the combined length exceeds maxKustomizationNameSuffixLength.
// Both values are read from annotations attached to the deployment.
func buildKustomizationNameSuffix(deployment *konfidencev1alpha1.ArtifactDeployment) string {
	ann := deployment.GetAnnotations()
	version := ann[pkgctrl.ArtifactVersionAnnotation]
	hash := ann[pkgctrl.ArtifactHashAnnotation]

	if full := version + "-" + hash; len(full)+1 <= maxKustomizationNameSuffixLength {
		return "-" + utils.SanitizeK8sResourceName(full)
	}
	return "-" + utils.SanitizeK8sResourceName(hash)
}
