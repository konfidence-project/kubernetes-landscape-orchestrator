package controller

import (
	"context"

	landscapev1alpha1 "github.com/konfidence-project/konfidence/api/star/v1alpha1"
)

// StatusUpdater interface defines global functions used to maintain the status of a resource.
type StatusUpdater interface {
	MutateStatus(ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment) error
}
