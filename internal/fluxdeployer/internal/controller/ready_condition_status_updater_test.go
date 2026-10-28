package controller

import (
	"context"
	"testing"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReadyConditionStatusUpdater_DoesNotOverrideStalledDeployment(t *testing.T) {
	deployment := &konfidencev1alpha1.ArtifactDeployment{
		ObjectMeta: metav1.ObjectMeta{Generation: 3},
		Status: konfidencev1alpha1.ArtifactDeploymentStatus{Conditions: []metav1.Condition{
			{Type: konfidencev1alpha1.AppHealthyCondition, Status: metav1.ConditionTrue},
			{
				Type:   konfidencev1alpha1.DeploymentResultCreatedCondition,
				Status: metav1.ConditionFalse,
				Reason: artifactDeploymentStalledReasonExposePortNotFound,
			},
			{
				Type:   konfidencev1alpha1.ArtifactDeploymentReadyCondition,
				Status: metav1.ConditionFalse,
				Reason: artifactDeploymentStalledReasonExposePortNotFound,
			},
			{
				Type:   konfidencev1alpha1.StalledCondition,
				Status: metav1.ConditionTrue,
				Reason: artifactDeploymentStalledReasonExposePortNotFound,
			},
		}},
	}

	updater := &ReadyConditionStatusUpdater{}
	if err := updater.MutateStatus(context.Background(), deployment); err != nil {
		t.Fatalf("MutateStatus: %v", err)
	}
	if meta.IsStatusConditionTrue(deployment.Status.Conditions, konfidencev1alpha1.ArtifactDeploymentReadyCondition) {
		t.Error("Ready: got true, want false while Stalled is true")
	}
}

func TestReadyConditionStatusUpdater_SetsReadyWhenHealthyAndResultsCreated(t *testing.T) {
	deployment := &konfidencev1alpha1.ArtifactDeployment{
		ObjectMeta: metav1.ObjectMeta{Generation: 3},
		Status: konfidencev1alpha1.ArtifactDeploymentStatus{Conditions: []metav1.Condition{
			{Type: konfidencev1alpha1.AppHealthyCondition, Status: metav1.ConditionTrue},
			{Type: konfidencev1alpha1.DeploymentResultCreatedCondition, Status: metav1.ConditionTrue},
		}},
	}

	updater := &ReadyConditionStatusUpdater{}
	if err := updater.MutateStatus(context.Background(), deployment); err != nil {
		t.Fatalf("MutateStatus: %v", err)
	}
	if !meta.IsStatusConditionTrue(deployment.Status.Conditions, konfidencev1alpha1.ArtifactDeploymentReadyCondition) {
		t.Error("Ready: got non-true, want true")
	}
}
