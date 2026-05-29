package controller

import (
	"context"

	landscapev1alpha1 "github.com/konfidence-project/konfidence/api/star/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ReadyConditionStatusUpdater struct {
}

func (r *ReadyConditionStatusUpdater) MutateStatus(_ context.Context, deployment *landscapev1alpha1.ArtifactDeployment) error {
	if meta.IsStatusConditionTrue(
		deployment.Status.Conditions,
		landscapev1alpha1.AppHealthyCondition,
	) && meta.IsStatusConditionTrue(
		deployment.Status.Conditions,
		landscapev1alpha1.DeploymentResultCreatedCondition,
	) {
		meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
			Type:               landscapev1alpha1.ArtifactDeploymentReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             landscapev1alpha1.ArtifactDeploymentReadyCondition,
			Message:            "Successfully reconciled ArtifactDeployment",
			ObservedGeneration: deployment.Generation,
			LastTransitionTime: metav1.Now(),
		})
	}
	return nil
}
