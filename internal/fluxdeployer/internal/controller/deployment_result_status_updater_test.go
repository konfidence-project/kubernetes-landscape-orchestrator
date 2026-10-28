package controller

import (
	"context"
	"encoding/json"
	"testing"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deploymentresult"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	candidatesResult = "candidates"
	apiPortName      = "api"
	missingName      = "missing"
	validResult      = "valid"
	dnsPortName      = "dns"
	artifactName     = "artifact"
)

func svc(name, ns string, annotations map[string]string, ports ...corev1.ServicePort) corev1.Service {
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Annotations: annotations},
		Spec:       corev1.ServiceSpec{Ports: ports},
	}
}

func tcpPort(name string, port int32) corev1.ServicePort {
	return corev1.ServicePort{Name: name, Port: port, Protocol: corev1.ProtocolTCP}
}

func serviceSpec(t *testing.T, result konfidencev1alpha1.DeploymentResult) deploymentresult.ServiceSpec {
	t.Helper()
	var spec deploymentresult.ServiceSpec
	if err := json.Unmarshal(result.Spec.Raw, &spec); err != nil {
		t.Fatalf("unmarshal spec for %q: %v", result.Name, err)
	}
	return spec
}

func TestMapServicesToDeploymentResults_SelectsNamedTCPPortAndPreservesAllPorts(t *testing.T) {
	list := &corev1.ServiceList{Items: []corev1.Service{
		svc("candidates-7f3a", "landscape", map[string]string{
			annotationDeploymentResult: candidatesResult,
			annotationExposePort:       apiPortName,
		}, tcpPort("admin", 8081), tcpPort(apiPortName, 80)),
	}}

	if err := validateExposePorts(list); err != nil {
		t.Fatalf("validateExposePorts: %v", err)
	}
	results, err := mapServicesToDeploymentResults(list)
	if err != nil {
		t.Fatalf("mapServicesToDeploymentResults: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results: got %d, want 1", len(results))
	}

	spec := serviceSpec(t, results[0])
	if spec.K8sName != "candidates-7f3a" || spec.Namespace != "landscape" {
		t.Errorf("spec identity: got %q/%q", spec.Namespace, spec.K8sName)
	}
	if len(spec.ServicePorts) != 2 {
		t.Fatalf("ServicePorts: got %d, want 2", len(spec.ServicePorts))
	}
	if spec.NorthSouthPort != apiPortName {
		t.Errorf("NorthSouthPort: got %q, want api", spec.NorthSouthPort)
	}
}

func TestMapServicesToDeploymentResults_EmptySelectionOptsOut(t *testing.T) {
	list := &corev1.ServiceList{Items: []corev1.Service{
		svc(missingName, "landscape", map[string]string{annotationDeploymentResult: missingName}, tcpPort("http", 80)),
		svc("empty", "landscape", map[string]string{
			annotationDeploymentResult: "empty",
			annotationExposePort:       "",
		}, tcpPort("http", 81)),
	}}

	if err := validateExposePorts(list); err != nil {
		t.Fatalf("validateExposePorts: %v", err)
	}
	results, err := mapServicesToDeploymentResults(list)
	if err != nil {
		t.Fatalf("mapServicesToDeploymentResults: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results: got %d, want 2", len(results))
	}
	for _, result := range results {
		spec := serviceSpec(t, result)
		if len(spec.ServicePorts) != 1 {
			t.Errorf("ServicePorts for %q: got %d, want 1", result.Name, len(spec.ServicePorts))
		}
		if spec.NorthSouthPort != "" {
			t.Errorf("NorthSouthPort for %q: got %q, want empty", result.Name, spec.NorthSouthPort)
		}
		var rawSpec map[string]any
		if err := json.Unmarshal(result.Spec.Raw, &rawSpec); err != nil {
			t.Fatalf("unmarshal raw spec for %q: %v", result.Name, err)
		}
		if _, ok := rawSpec["NorthSouthPort"]; ok {
			t.Errorf("serialized NorthSouthPort for %q: got present, want omitted", result.Name)
		}
	}
}

func TestMapServicesToDeploymentResults_IgnoresMissingOrEmptyIdentityBeforeExposure(t *testing.T) {
	list := &corev1.ServiceList{Items: []corev1.Service{
		svc(missingName, "landscape", map[string]string{annotationExposePort: missingName}, tcpPort("http", 80)),
		svc("empty", "landscape", map[string]string{
			annotationDeploymentResult: "",
			annotationExposePort:       missingName,
		}, tcpPort("http", 81)),
	}}

	if err := validateExposePorts(list); err != nil {
		t.Fatalf("validateExposePorts: %v", err)
	}
	results, err := mapServicesToDeploymentResults(list)
	if err != nil {
		t.Fatalf("mapServicesToDeploymentResults: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results: got %d, want 0", len(results))
	}
}

func TestValidateExposePorts_ReturnsFirstInvalidSelection(t *testing.T) {
	list := &corev1.ServiceList{Items: []corev1.Service{
		svc(validResult, "landscape", map[string]string{
			annotationDeploymentResult: validResult,
			annotationExposePort:       apiPortName,
		}, tcpPort("admin", 8081), tcpPort(apiPortName, 80)),
		svc(missingName, "landscape", map[string]string{
			annotationDeploymentResult: missingName,
			annotationExposePort:       " api ",
		}, tcpPort("admin", 8081), tcpPort(apiPortName, 80)),
		svc("udp", "landscape", map[string]string{
			annotationDeploymentResult: "udp",
			annotationExposePort:       dnsPortName,
		}, corev1.ServicePort{Name: dnsPortName, Port: 53, Protocol: corev1.ProtocolUDP}),
	}}

	err := validateExposePorts(list)
	if err == nil {
		t.Fatal("validateExposePorts: got nil, want invalid selection")
	}
	if err.reason != artifactDeploymentStalledReasonExposePortNotFound {
		t.Errorf("reason: got %q, want %q", err.reason, artifactDeploymentStalledReasonExposePortNotFound)
	}
	wantMessage := "Service \"missing\" selects expose-port \" api \", but no port has that exact name " +
		"(available ports: \"admin\"/TCP, \"api\"/TCP)"
	if err.Error() != wantMessage {
		t.Errorf("message: got %q, want %q", err.Error(), wantMessage)
	}
}

func TestValidateExposePorts_MatchesPortNamesExactly(t *testing.T) {
	for _, selectedPort := range []string{"API", " api ", "80"} {
		t.Run(selectedPort, func(t *testing.T) {
			list := &corev1.ServiceList{Items: []corev1.Service{
				svc("service", "landscape", map[string]string{
					annotationDeploymentResult: "result",
					annotationExposePort:       selectedPort,
				}, tcpPort(apiPortName, 80)),
			}}

			err := validateExposePorts(list)
			if err == nil || err.reason != artifactDeploymentStalledReasonExposePortNotFound {
				t.Fatalf("validateExposePorts: got %#v, want not-found error", err)
			}
		})
	}
}

func TestValidateExposePorts_UnsupportedProtocolDeterminesReason(t *testing.T) {
	tests := []struct {
		name     string
		portName string
		port     int32
		protocol corev1.Protocol
		message  string
	}{
		{
			name:     "UDP",
			portName: dnsPortName,
			port:     53,
			protocol: corev1.ProtocolUDP,
			message:  `Service "dns" selects expose-port "dns" with unsupported protocol "UDP" (only TCP is supported)`,
		},
		{
			name:     "SCTP",
			portName: "events",
			port:     9000,
			protocol: corev1.ProtocolSCTP,
			message:  `Service "events" selects expose-port "events" with unsupported protocol "SCTP" (only TCP is supported)`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			list := &corev1.ServiceList{Items: []corev1.Service{
				svc(test.portName, "landscape", map[string]string{
					annotationDeploymentResult: test.portName,
					annotationExposePort:       test.portName,
				}, corev1.ServicePort{Name: test.portName, Port: test.port, Protocol: test.protocol}),
			}}

			err := validateExposePorts(list)
			if err == nil {
				t.Fatal("validateExposePorts: got nil, want unsupported protocol")
			}
			if err.reason != artifactDeploymentStalledReasonExposePortUnsupportedProtocol {
				t.Errorf("reason: got %q, want %q", err.reason, artifactDeploymentStalledReasonExposePortUnsupportedProtocol)
			}
			if err.Error() != test.message {
				t.Errorf("message: got %q, want %q", err.Error(), test.message)
			}
		})
	}
}

func TestValidateExposePorts_DoesNotSelectUnnamedPortImplicitly(t *testing.T) {
	list := &corev1.ServiceList{Items: []corev1.Service{
		svc("service", "landscape", map[string]string{
			annotationDeploymentResult: "result",
			annotationExposePort:       "http",
		}, tcpPort("", 8080)),
	}}

	err := validateExposePorts(list)
	if err == nil {
		t.Fatal("validateExposePorts: got nil, want not-found error")
	}
	if err.reason != artifactDeploymentStalledReasonExposePortNotFound {
		t.Errorf("reason: got %q, want %q", err.reason, artifactDeploymentStalledReasonExposePortNotFound)
	}
	wantMessage := `Service "service" selects expose-port "http", but no port has that exact name (available ports: ""/TCP)`
	if err.Error() != wantMessage {
		t.Errorf("message: got %q, want %q", err.Error(), wantMessage)
	}
}

func TestMapServicesToDeploymentResults_RejectsDuplicateNameType(t *testing.T) {
	list := &corev1.ServiceList{Items: []corev1.Service{
		svc("candidates-a1", "shop", map[string]string{annotationDeploymentResult: candidatesResult}, tcpPort("http", 80)),
		svc("candidates-b2", "shop", map[string]string{annotationDeploymentResult: candidatesResult}, tcpPort("http", 80)),
	}}

	if _, err := mapServicesToDeploymentResults(list); err == nil {
		t.Fatal("expected error for duplicate (name, type), got nil")
	}
}

func TestDeploymentResultStatusUpdater_RejectsAllResultsAndPublishesExposureFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := konfidencev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add Konfidence scheme: %v", err)
	}

	deployment := &konfidencev1alpha1.ArtifactDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: artifactName, Namespace: "landscape", Generation: 4},
		Status: konfidencev1alpha1.ArtifactDeploymentStatus{Conditions: []metav1.Condition{{
			Type:   konfidencev1alpha1.ArtifactDeployedCondition,
			Status: metav1.ConditionTrue,
		}}},
	}
	service := svc(apiPortName, "landscape", map[string]string{
		annotationDeploymentResult: apiPortName,
		annotationExposePort:       missingName,
	}, tcpPort("http", 80))
	service.Labels = map[string]string{labelArtifactDeployment: artifactName}
	validService := svc("valid", "landscape", map[string]string{
		annotationDeploymentResult: validResult,
		annotationExposePort:       apiPortName,
	}, tcpPort(apiPortName, 80))
	validService.Labels = map[string]string{labelArtifactDeployment: artifactName}

	updater := &DeploymentResultStatusUpdater{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(&service, &validService).Build(),
	}
	if err := updater.MutateStatus(context.Background(), deployment); err != nil {
		t.Fatalf("MutateStatus: %v", err)
	}

	if len(deployment.Status.DeploymentResults) != 0 {
		t.Fatalf("DeploymentResults: got %d, want none", len(deployment.Status.DeploymentResults))
	}
	created := meta.FindStatusCondition(deployment.Status.Conditions, konfidencev1alpha1.DeploymentResultCreatedCondition)
	if created == nil || created.Status != metav1.ConditionFalse || created.Reason != artifactDeploymentStalledReasonExposePortNotFound {
		t.Errorf("DeploymentResultCreated: got %#v, want False/%s", created, artifactDeploymentStalledReasonExposePortNotFound)
	}
	ready := meta.FindStatusCondition(deployment.Status.Conditions, konfidencev1alpha1.ArtifactDeploymentReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != artifactDeploymentStalledReasonExposePortNotFound {
		t.Errorf("Ready: got %#v, want False/%s", ready, artifactDeploymentStalledReasonExposePortNotFound)
	}
	stalled := meta.FindStatusCondition(deployment.Status.Conditions, konfidencev1alpha1.StalledCondition)
	if stalled == nil || stalled.Status != metav1.ConditionTrue || stalled.Reason != artifactDeploymentStalledReasonExposePortNotFound {
		t.Errorf("Stalled: got %#v, want True/%s", stalled, artifactDeploymentStalledReasonExposePortNotFound)
	}
}

func TestDeploymentResultStatusUpdater_DoesNotRevalidateExposureFailure(t *testing.T) {
	deployment := &konfidencev1alpha1.ArtifactDeployment{
		Status: konfidencev1alpha1.ArtifactDeploymentStatus{
			Conditions: []metav1.Condition{
				{
					Type:   konfidencev1alpha1.DeploymentResultCreatedCondition,
					Status: metav1.ConditionFalse,
					Reason: artifactDeploymentStalledReasonExposePortUnsupportedProtocol,
				},
				{
					Type:   konfidencev1alpha1.StalledCondition,
					Status: metav1.ConditionTrue,
					Reason: artifactDeploymentStalledReasonExposePortUnsupportedProtocol,
				},
			},
		},
	}

	updater := &DeploymentResultStatusUpdater{}
	if err := updater.MutateStatus(context.Background(), deployment); err != nil {
		t.Fatalf("MutateStatus: %v", err)
	}
	if len(deployment.Status.DeploymentResults) != 0 {
		t.Fatalf("DeploymentResults: got %#v, want none", deployment.Status.DeploymentResults)
	}
}
