package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

// errDeploymentResultNotUnique marks two Services declaring the same (name, type); only a change to the deployed
// manifests resolves it.
var errDeploymentResultNotUnique = errors.New("deployment result (name, type) is not unique")

// reasonDeploymentResultInvalid is the DeploymentResultCreated=False reason when the opted-in Services can't be mapped.
const reasonDeploymentResultInvalid = "DeploymentResultInvalid"

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

	if validationErr := validateExposePorts(serviceList); validationErr != nil {
		setDeploymentResultFailure(deployment, validationErr.reason, validationErr.Error())
		return nil
	}

	deploymentResultServices, err := mapServicesToDeploymentResults(serviceList)
	if err != nil {
		reason := reasonDeploymentResultInvalid
		if errors.Is(err, errDeploymentResultNotUnique) {
			reason = konfidencev1alpha1.ArtifactDeploymentStalledReasonDeploymentResultNotUnique
		}
		setDeploymentResultFailure(deployment, reason, err.Error())
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

// setDeploymentResultFailure withdraws the published results. Ready and Stalled follow from the reason.
func setDeploymentResultFailure(deployment *konfidencev1alpha1.ArtifactDeployment, reason, message string) {
	deployment.Status.DeploymentResults = nil
	meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
		Type:               konfidencev1alpha1.DeploymentResultCreatedCondition,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            truncateMessage(message),
		ObservedGeneration: deployment.Generation,
	})
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

// servicesByName orders Services by name, so a failure always names the same Services.
func servicesByName(serviceList *corev1.ServiceList) []*corev1.Service {
	services := make([]*corev1.Service, 0, len(serviceList.Items))
	for i := range serviceList.Items {
		services = append(services, &serviceList.Items[i])
	}
	slices.SortFunc(services, func(a, b *corev1.Service) int { return strings.Compare(a.Name, b.Name) })
	return services
}

func hasDeploymentResultName(service *corev1.Service) bool {
	if service.Annotations[annotationDeploymentResult] == "" {
		// TODO: Services with `konfidence.cloud/exposed-port-name` but no deployment-result identity are ignored. Revisit
		// this when such Services have a stable deployment-result identity.
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
	for _, service := range servicesByName(serviceList) {
		if !hasDeploymentResultName(service) {
			continue
		}

		selectedPort := service.Annotations[annotationExposedPortName]
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
					"Service %q: annotation %q names port %q, but no Service port has that name (available ports: %s)",
					service.Name, annotationExposedPortName, selectedPort, formatAvailablePorts(service.Spec.Ports)),
			}
		case matchedPort.Protocol != corev1.ProtocolTCP:
			return &exposureValidationError{
				reason: artifactDeploymentStalledReasonExposePortUnsupportedProtocol,
				message: fmt.Sprintf(
					"Service %q: annotation %q names port %q, which uses unsupported protocol %q (only TCP is supported)",
					service.Name, annotationExposedPortName, selectedPort, matchedPort.Protocol),
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

	for _, service := range servicesByName(serviceList) {
		if !hasDeploymentResultName(service) {
			continue
		}
		resultName := service.Annotations[annotationDeploymentResult]

		if first, dup := seen[resultName+"\x00"+deploymentresult.TypeHTTPK8sService]; dup {
			return nil, fmt.Errorf(
				"%w: services %q and %q both declare (name=%q, type=%q)",
				errDeploymentResultNotUnique, first, service.Name, resultName, deploymentresult.TypeHTTPK8sService)
		}
		seen[resultName+"\x00"+deploymentresult.TypeHTTPK8sService] = service.Name

		deploymentResultServiceSpecRaw, err := json.Marshal(deploymentresult.ServiceSpec{
			Namespace:              service.Namespace,
			K8sName:                service.Name,
			ServicePorts:           service.Spec.Ports,
			IngressExposedPortName: service.Annotations[annotationExposedPortName],
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
