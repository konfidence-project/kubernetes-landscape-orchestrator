package controller

import (
	"context"
	"testing"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func condition(conditionType string, status metav1.ConditionStatus, reason string) metav1.Condition {
	return metav1.Condition{Type: conditionType, Status: status, Reason: reason, Message: "from " + reason, LastTransitionTime: metav1.Now()}
}

func TestReadyConditionStatusUpdater(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		wantStatus metav1.ConditionStatus
		wantReason string
	}{
		{
			name: "healthy with results is ready",
			conditions: []metav1.Condition{
				condition(konfidencev1alpha1.ArtifactFetchedCondition, metav1.ConditionTrue, "Fetched"),
				condition(konfidencev1alpha1.ArtifactDeployedCondition, metav1.ConditionTrue, "Deployed"),
				condition(konfidencev1alpha1.AppHealthyCondition, metav1.ConditionTrue, "Healthy"),
				condition(konfidencev1alpha1.DeploymentResultCreatedCondition, metav1.ConditionTrue, "Created"),
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: konfidencev1alpha1.ArtifactDeploymentReadyCondition,
		},
		{
			name:       "nothing reported yet",
			wantStatus: metav1.ConditionFalse,
			wantReason: reasonPending,
		},
		{
			name: "carries the reason of the first failing step",
			conditions: []metav1.Condition{
				condition(konfidencev1alpha1.ArtifactFetchedCondition, metav1.ConditionTrue, "Fetched"),
				condition(konfidencev1alpha1.ArtifactDeployedCondition, metav1.ConditionFalse, "InstallFailed"),
				condition(konfidencev1alpha1.AppHealthyCondition, metav1.ConditionFalse, "Unhealthy"),
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: "InstallFailed",
		},
		{
			name: "goes false again when the deployment results break",
			conditions: []metav1.Condition{
				condition(konfidencev1alpha1.ArtifactFetchedCondition, metav1.ConditionTrue, "Fetched"),
				condition(konfidencev1alpha1.ArtifactDeployedCondition, metav1.ConditionTrue, "Deployed"),
				condition(konfidencev1alpha1.AppHealthyCondition, metav1.ConditionTrue, "Healthy"),
				condition(konfidencev1alpha1.DeploymentResultCreatedCondition, metav1.ConditionFalse, reasonDeploymentResultInvalid),
				condition(konfidencev1alpha1.ArtifactDeploymentReadyCondition, metav1.ConditionTrue, "Ready"),
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: reasonDeploymentResultInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := &konfidencev1alpha1.ArtifactDeployment{}
			deployment.Status.Conditions = tt.conditions
			if err := (&ReadyConditionStatusUpdater{}).MutateStatus(context.Background(), deployment); err != nil {
				t.Fatalf("MutateStatus: %v", err)
			}
			ready := meta.FindStatusCondition(deployment.Status.Conditions, konfidencev1alpha1.ArtifactDeploymentReadyCondition)
			if ready == nil {
				t.Fatal("Ready not written")
			}
			if ready.Status != tt.wantStatus || ready.Reason != tt.wantReason {
				t.Errorf("Ready: got %s/%s, want %s/%s", ready.Status, ready.Reason, tt.wantStatus, tt.wantReason)
			}
		})
	}
}
