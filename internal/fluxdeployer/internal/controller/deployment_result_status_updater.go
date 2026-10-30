package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	if hasExposureValidationFailure(deployment) {
		return nil
	}
	if !meta.IsStatusConditionTrue(deployment.Status.Conditions, konfidencev1alpha1.ArtifactDeployedCondition) {
		return nil
	}
	// detect exposable services of flux deployment by label
	serviceList, err := d.fetchExposableServices(ctx, deployment)
	if err != nil {
		return err
	}

	if validationErr := validateExposePorts(serviceList); validationErr != nil {
		setExposureValidationFailure(deployment, validationErr)
		return nil
	}

	deploymentResultServices, err := mapServicesToDeploymentResults(serviceList)
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

func setExposureValidationFailure(
	deployment *konfidencev1alpha1.ArtifactDeployment,
	validationErr *exposureValidationError,
) {
	deployment.Status.DeploymentResults = nil
	now := metav1.Now()
	for _, condition := range []metav1.Condition{
		{
			Type:   konfidencev1alpha1.DeploymentResultCreatedCondition,
			Status: metav1.ConditionFalse,
		},
		{
			Type:   konfidencev1alpha1.ArtifactDeploymentReadyCondition,
			Status: metav1.ConditionFalse,
		},
		{
			Type:   konfidencev1alpha1.StalledCondition,
			Status: metav1.ConditionTrue,
		},
	} {
		condition.Reason = validationErr.reason
		condition.Message = validationErr.Error()
		condition.ObservedGeneration = deployment.Generation
		condition.LastTransitionTime = now
		meta.SetStatusCondition(&deployment.Status.Conditions, condition)
	}
}

func hasExposureValidationFailure(deployment *konfidencev1alpha1.ArtifactDeployment) bool {
	stalled := meta.FindStatusCondition(deployment.Status.Conditions, konfidencev1alpha1.StalledCondition)
	if stalled == nil || stalled.Status != metav1.ConditionTrue {
		return false
	}
	return stalled.Reason == artifactDeploymentStalledReasonExposePortNotFound ||
		stalled.Reason == artifactDeploymentStalledReasonExposePortUnsupportedProtocol
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

func hasDeploymentResultName(service *corev1.Service) bool {
	if service.Annotations[annotationDeploymentResult] == "" {
		// TODO: Services carrying expose-port without a usable deployment-result identity are intentionally ignored
		// for now. Revisit this behavior when the producer contract gains a stable identity for such Services.
		return false
	}
	return true
}

type exposureValidationError struct {
	reason  string
	message string
}

func (e *exposureValidationError) Error() string {
	return e.message
}

func validateExposePorts(serviceList *corev1.ServiceList) *exposureValidationError {
	for i := range serviceList.Items {
		service := &serviceList.Items[i]
		if !hasDeploymentResultName(service) {
			continue
		}

		selectedPort := service.Annotations[annotationExposePort]
		if selectedPort == "" {
			continue
		}

		var matchedPort *corev1.ServicePort
		for portIndex := range service.Spec.Ports {
			if service.Spec.Ports[portIndex].Name == selectedPort {
				matchedPort = &service.Spec.Ports[portIndex]
				break
			}
		}
		switch {
		case matchedPort == nil:
			return &exposureValidationError{
				reason: artifactDeploymentStalledReasonExposePortNotFound,
				message: fmt.Sprintf(
					"Service %q selects expose-port %q, but no port has that exact name (available ports: %s)",
					service.Name, selectedPort, formatAvailablePorts(service.Spec.Ports)),
			}
		case matchedPort.Protocol != corev1.ProtocolTCP:
			return &exposureValidationError{
				reason: artifactDeploymentStalledReasonExposePortUnsupportedProtocol,
				message: fmt.Sprintf(
					"Service %q selects expose-port %q with unsupported protocol %q (only TCP is supported)",
					service.Name, selectedPort, matchedPort.Protocol),
			}
		}
	}
	return nil
}

// mapServicesToDeploymentResults turns validated, opted-in Services into DeploymentResults. A Service opts in via the
// annotationDeploymentResult annotation, whose value becomes the stable result Name; Services without it are ignored.
// Results are identified by (Name, Type); two Services yielding the same pair is a misconfiguration and is rejected.
func mapServicesToDeploymentResults(serviceList *corev1.ServiceList) ([]konfidencev1alpha1.DeploymentResult, error) {
	deploymentResultServices := make([]konfidencev1alpha1.DeploymentResult, 0, len(serviceList.Items))
	seen := make(map[string]string, len(serviceList.Items))

	for i := range serviceList.Items {
		service := &serviceList.Items[i]
		if !hasDeploymentResultName(service) {
			continue
		}
		resultName := service.Annotations[annotationDeploymentResult]

		if first, dup := seen[resultName+"\x00"+deploymentresult.TypeHTTPK8sService]; dup {
			return nil, fmt.Errorf(
				"services %q and %q both declare deployment result (name=%q, type=%q); the pair must be unique",
				first, service.Name, resultName, deploymentresult.TypeHTTPK8sService)
		}
		seen[resultName+"\x00"+deploymentresult.TypeHTTPK8sService] = service.Name

		deploymentResultServiceSpecRaw, err := json.Marshal(deploymentresult.ServiceSpec{
			Namespace:      service.Namespace,
			K8sName:        service.Name,
			ServicePorts:   service.Spec.Ports,
			NorthSouthPort: service.Annotations[annotationExposePort],
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

func formatAvailablePorts(ports []corev1.ServicePort) string {
	if len(ports) == 0 {
		return "none"
	}
	available := make([]string, 0, len(ports))
	for _, port := range ports {
		available = append(available, fmt.Sprintf("%q/%s", port.Name, port.Protocol))
	}
	return strings.Join(available, ", ")
}
