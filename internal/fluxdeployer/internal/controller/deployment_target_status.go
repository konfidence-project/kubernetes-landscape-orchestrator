package controller

import (
	"errors"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ArtifactDeploymentReasonDeploymentTargetNotReady indicates that no ready target exists.
const ArtifactDeploymentReasonDeploymentTargetNotReady = "DeploymentTargetNotReady"

func setDeploymentTargetNotReady(deployment *konfidencev1alpha1.ArtifactDeployment, err error) bool {
	var targetErr *DeploymentTargetNotReadyError
	if !errors.As(err, &targetErr) {
		return false
	}

	meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
		Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
		Status:             metav1.ConditionFalse,
		Reason:             ArtifactDeploymentReasonDeploymentTargetNotReady,
		Message:            targetErr.Error(),
		ObservedGeneration: deployment.Generation,
		LastTransitionTime: metav1.Now(),
	})
	return true
}
