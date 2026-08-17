package controller

import (
	"context"
	"encoding/json"
	"fmt"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd/utils"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deploymentresult"
	"k8s.io/apimachinery/pkg/api/meta"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeploymentResultStatusUpdater struct {
	client.Client
}

// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch

func (d *DeploymentResultStatusUpdater) MutateStatus(ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment) error {
	if !meta.IsStatusConditionTrue(deployment.Status.Conditions, konfidencev1alpha1.ArtifactDeployedCondition) {
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
		Type:               konfidencev1alpha1.DeploymentResultCreatedCondition,
		Status:             metav1.ConditionTrue,
		Reason:             konfidencev1alpha1.DeploymentResultCreatedCondition,
		Message:            "Successfully created DeploymentResult",
		ObservedGeneration: deployment.Generation,
		LastTransitionTime: metav1.Now(),
	})

	return nil
}

func (s *DeploymentResultStatusUpdater) fetchExposableServices(
	ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment,
) (*corev1.ServiceList, error) {
	serviceSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			labelArtifactDeployment: utils.SanitizeK8sResourceName(deployment.Name),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Selector: %w", err)
	}

	serviceList := &corev1.ServiceList{}
	if err := s.List(ctx, serviceList, client.InNamespace(deployment.Namespace), client.MatchingLabelsSelector{Selector: serviceSelector}); err != nil {
		return nil, fmt.Errorf("failed to list services by label selector: %w", err)
	}
	return serviceList, nil
}

// mapServicesToDeploymentResult turns the opted-in Services into DeploymentResults. A Service opts in via the
// annotationDeploymentResult annotation, whose value becomes the stable result Name; Services without it are ignored.
// Results are identified by (Name, Type); two Services yielding the same pair is a misconfiguration and is rejected.
func (s *DeploymentResultStatusUpdater) mapServicesToDeploymentResult(serviceList *corev1.ServiceList) ([]konfidencev1alpha1.DeploymentResult, error) {
	deploymentResultServices := make([]konfidencev1alpha1.DeploymentResult, 0, len(serviceList.Items))
	seen := make(map[string]string, len(serviceList.Items))

	for i := range serviceList.Items {
		service := &serviceList.Items[i]
		resultName, ok := service.Annotations[annotationDeploymentResult]
		if !ok {
			continue
		}

		if first, dup := seen[resultName+"\x00"+deploymentresult.TypeHTTPK8sService]; dup {
			return nil, fmt.Errorf(
				"services %q and %q both declare deployment result (name=%q, type=%q); the pair must be unique",
				first, service.Name, resultName, deploymentresult.TypeHTTPK8sService)
		}
		seen[resultName+"\x00"+deploymentresult.TypeHTTPK8sService] = service.Name

		deploymentResultServiceSpecRaw, err := json.Marshal(deploymentresult.ServiceSpec{
			Namespace:    service.Namespace,
			K8sName:      service.Name,
			ServicePorts: service.Spec.Ports,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal deploymentResultServiceSpec: %w", err)
		}

		deploymentResultServices = append(deploymentResultServices, konfidencev1alpha1.DeploymentResult{
			Name: resultName,
			Type: deploymentresult.TypeHTTPK8sService,
			Spec: runtime.RawExtension{Raw: deploymentResultServiceSpecRaw},
		})
	}
	return deploymentResultServices, nil
}
