package controller

import (
	"context"
	"encoding/json"
	"fmt"

	landscapev1alpha1 "github.com/konfidence-project/konfidence/api/star/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd/utils"
	"k8s.io/apimachinery/pkg/api/meta"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeploymentResultStatusUpdater struct {
	client.Client
}

type DeploymentResultServiceSpec struct {
	Namespace    string
	K8sName      string
	ServicePorts []corev1.ServicePort
}

func (d *DeploymentResultStatusUpdater) MutateStatus(ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment) error {
	if !meta.IsStatusConditionTrue(deployment.Status.Conditions, landscapev1alpha1.ArtifactDeployedCondition) {
		return nil
	}
	// detect exposable services of flux deployment by label
	serviceList, err := d.fetchExposableServices(ctx, deployment)
	if err != nil {
		return err
	}

	// map Services to DeploymentResult
	deploymentResultServices, err := d.mapServicesToDeploymentResult(serviceList)
	if err != nil {
		return err
	}

	deployment.Status.DeploymentResults = deploymentResultServices

	meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
		Type:               landscapev1alpha1.DeploymentResultCreatedCondition,
		Status:             metav1.ConditionTrue,
		Reason:             landscapev1alpha1.DeploymentResultCreatedCondition,
		Message:            "Successfully created DeploymentResult",
		ObservedGeneration: deployment.Generation,
		LastTransitionTime: metav1.Now(),
	})

	return nil
}

func (s *DeploymentResultStatusUpdater) fetchExposableServices(
	ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment,
) (*corev1.ServiceList, error) {
	serviceLabelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"konfidence.cloud/artifact-deployment": utils.SanitizeK8sResourceName(deployment.Name),
		},
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				// TODO (sascha 05.12.25): konfidence specific label "konfidence.cloud/appname" in
				// deployment-manifests (Helm-charts, kustomize) has to be removed.
				// Solution comes with https://github.com/konfidence-project/konfidence-project/issues/299
				Key:      "konfidence.cloud/appname",
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	}

	serviceSelector, err := metav1.LabelSelectorAsSelector(serviceLabelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to create Selector: %w", err)
	}

	serviceList := &corev1.ServiceList{}
	if err := s.List(ctx, serviceList, client.InNamespace(deployment.Namespace), client.MatchingLabelsSelector{Selector: serviceSelector}); err != nil {
		return nil, fmt.Errorf("failed to list services by label selector: %w", err)
	}
	return serviceList, nil
}

func (s *DeploymentResultStatusUpdater) mapServicesToDeploymentResult(serviceList *corev1.ServiceList) ([]landscapev1alpha1.DeploymentResult, error) {
	deploymentResultServices := make([]landscapev1alpha1.DeploymentResult, len(serviceList.Items))

	for i, service := range serviceList.Items {
		deploymentResultServiceSpec := DeploymentResultServiceSpec{
			Namespace:    service.Namespace,
			K8sName:      service.Name,
			ServicePorts: service.Spec.Ports,
		}

		deploymentResultServiceSpecRaw, err := json.Marshal(deploymentResultServiceSpec)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal deploymentResultServiceSpec: %w", err)
		}

		deploymentResultServices[i] = landscapev1alpha1.DeploymentResult{
			Name: service.Labels["konfidence.cloud/appname"],
			Type: "http-k8s-service",
			Spec: runtime.RawExtension{Raw: deploymentResultServiceSpecRaw},
		}
	}
	return deploymentResultServices, nil
}
