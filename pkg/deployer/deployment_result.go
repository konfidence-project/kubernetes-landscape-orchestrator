package deployer

import (
	"context"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
)

type DeploymentResulter interface {
	GetDeploymentResults(ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment) ([]konfidencev1alpha1.DeploymentResult, error)
}
