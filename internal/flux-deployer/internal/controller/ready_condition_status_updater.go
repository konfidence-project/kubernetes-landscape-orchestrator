/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ReadyConditionStatusUpdater struct {
}

func (r *ReadyConditionStatusUpdater) MutateStatus(_ context.Context, deployment *landscapev1alpha1.ArtifactDeployment) error {
	if meta.IsStatusConditionTrue(deployment.Status.Conditions, landscapev1alpha1.AppHealthyCondition) && meta.IsStatusConditionTrue(deployment.Status.Conditions, landscapev1alpha1.DeploymentResultCreatedCondition) {
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
