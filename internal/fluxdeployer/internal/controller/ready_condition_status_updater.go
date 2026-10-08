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
	if meta.IsStatusConditionTrue(deployment.Status.Conditions, konfidencev1alpha1.AppHealthyCondition) &&
		meta.IsStatusConditionTrue(deployment.Status.Conditions, konfidencev1alpha1.DeploymentResultCreatedCondition) {
		meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
			Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             konfidencev1alpha1.ArtifactDeploymentReadyCondition,
			Message:            "Successfully reconciled ArtifactDeployment",
			ObservedGeneration: deployment.Generation,
			LastTransitionTime: metav1.Now(),
		})
		return nil
	}

	reason, message := notReadyCause(deployment.Status.Conditions)
	meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
		Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: deployment.Generation,
	})
	return nil
}

func notReadyCause(conditions []metav1.Condition) (reason, message string) {
	for _, conditionType := range pipelineConditions {
		condition := meta.FindStatusCondition(conditions, conditionType)
		if condition == nil {
			return reasonPending, fmt.Sprintf("%s has not been reported yet", conditionType)
		}
		if condition.Status != metav1.ConditionTrue {
			return condition.Reason, fmt.Sprintf("%s is %s: %s", conditionType, condition.Status, condition.Message)
		}
	}
	// unreachable: MutateStatus only asks when AppHealthy or DeploymentResultCreated is not True
	return reasonPending, "waiting for the deployment pipeline"
}
