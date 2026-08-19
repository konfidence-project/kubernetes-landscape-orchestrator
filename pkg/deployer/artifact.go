// Package deployer defines runtime-independent contracts for Konfidence deployers.
package deployer

import (
	"context"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
)

// ArtifactDeployer reconciles the runtime-specific resources for an ArtifactDeployment.
type ArtifactDeployer interface {
	Reconcile(ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment) error
}
