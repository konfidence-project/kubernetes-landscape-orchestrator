package controller

import (
	"context"
	"fmt"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// reasonPending is the Ready=False reason while a pipeline step has not reported yet.
const reasonPending = "Pending"

// pipelineConditions are the steps an ArtifactDeployment goes through, in order.
var pipelineConditions = []string{
	konfidencev1alpha1.ArtifactFetchedCondition,
	konfidencev1alpha1.ArtifactDeployedCondition,
	konfidencev1alpha1.AppHealthyCondition,
	konfidencev1alpha1.DeploymentResultCreatedCondition,
}

type ReadyConditionStatusUpdater struct {
}

// MutateStatus sets Ready=True once the app is healthy and its deployment results are created, and Ready=False
// otherwise, carrying the reason of the first pipeline step that is not True.
func (r *ReadyConditionStatusUpdater) MutateStatus(_ context.Context, deployment *konfidencev1alpha1.ArtifactDeployment) error {
	conditions := deployment.Status.Conditions
	if meta.IsStatusConditionTrue(conditions, konfidencev1alpha1.AppHealthyCondition) &&
		meta.IsStatusConditionTrue(conditions, konfidencev1alpha1.DeploymentResultCreatedCondition) {
		setReady(deployment, metav1.ConditionTrue, konfidencev1alpha1.ArtifactDeploymentReadyCondition,
			"Successfully reconciled ArtifactDeployment")
		return nil
	}

	for _, conditionType := range pipelineConditions {
		step := meta.FindStatusCondition(conditions, conditionType)
		switch {
		case step == nil:
			setReady(deployment, metav1.ConditionFalse, reasonPending, fmt.Sprintf("%s has not been reported yet", conditionType))
			return nil
		case step.Status == metav1.ConditionTrue:
			continue
		case step.Status == metav1.ConditionUnknown &&
			meta.IsStatusConditionTrue(conditions, konfidencev1alpha1.ArtifactDeploymentReadyCondition):
			// Flux marks its conditions Unknown at the start of every reconcile, so keep Ready until a step fails
			return nil
		default:
			setReady(deployment, metav1.ConditionFalse, step.Reason,
				fmt.Sprintf("%s is %s: %s", conditionType, step.Status, step.Message))
			return nil
		}
	}
	return nil
}

func setReady(deployment *konfidencev1alpha1.ArtifactDeployment, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
		Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: deployment.Generation,
	})
}
