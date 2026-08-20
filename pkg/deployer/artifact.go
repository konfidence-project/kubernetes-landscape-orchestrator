// Package deployer defines runtime-independent contracts for Konfidence deployers.
package deployer

import (
	"context"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ReconcileResult struct {
	Result            reconcile.Result
	Conditions        []metav1.Condition
	DeploymentResults []konfidencev1alpha1.DeploymentResult
}

// ArtifactReconciler reconciles the runtime-specific resources for an ArtifactDeployment.
type ArtifactReconciler interface {
	Reconcile(ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment) (ReconcileResult, error)
}
