package controller

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deploymentresult"
)

const (
	candidatesResult              = "candidates"
	ingressExposedAnnotationValue = "true"
)

func svc(name, ns string, annotations map[string]string, ports ...corev1.ServicePort) corev1.Service {
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Annotations: annotations},
		Spec:       corev1.ServiceSpec{Ports: ports},
	}
}

// TestMapServicesToDeploymentResult_OptInByAnnotation: only Services carrying annotationDeploymentResult become
// results, DR.Name is the annotation value, and the full Service spec (incl. all ports) is carried verbatim.
func TestMapServicesToDeploymentResult_OptInByAnnotation(t *testing.T) {
	u := &DeploymentResultStatusUpdater{}
	list := &corev1.ServiceList{Items: []corev1.Service{
		svc("candidates-7f3a", "landscape", map[string]string{
			annotationDeploymentResult: candidatesResult,
			annotationIngressExposed:   ingressExposedAnnotationValue,
		},
			corev1.ServicePort{Name: "http", Port: 80},
			corev1.ServicePort{Name: "grpc", Port: 9090}),
		svc("no-optin", "landscape", map[string]string{annotationIngressExposed: ingressExposedAnnotationValue}, corev1.ServicePort{Port: 80}),
	}}

	results, err := u.mapServicesToDeploymentResult(list)
	if err != nil {
		t.Fatalf("mapServicesToDeploymentResult: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results: got %d, want 1 (only the annotated Service)", len(results))
	}
	if results[0].Name != candidatesResult {
		t.Errorf("DR.Name: got %q, want %q (annotation value)", results[0].Name, candidatesResult)
	}
	if results[0].Type != deploymentresult.TypeHTTPK8sService {
		t.Errorf("DR.Type: got %q", results[0].Type)
	}
	var spec deploymentresult.ServiceSpec
	if err := json.Unmarshal(results[0].Spec.Raw, &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	if spec.K8sName != "candidates-7f3a" || spec.Namespace != "landscape" {
		t.Errorf("spec identity: got %q/%q", spec.Namespace, spec.K8sName)
	}
	if len(spec.ServicePorts) != 2 {
		t.Errorf("multi-port not preserved: got %d ports, want 2", len(spec.ServicePorts))
	}
	if !spec.IngressExposed {
		t.Error("IngressExposed: got false, want true")
	}
}

func TestMapServicesToDeploymentResult_IngressExposureRequiresTrue(t *testing.T) {
	u := &DeploymentResultStatusUpdater{}
	list := &corev1.ServiceList{Items: []corev1.Service{
		svc("missing", "landscape", map[string]string{annotationDeploymentResult: "missing"}),
		svc("false", "landscape", map[string]string{
			annotationDeploymentResult: "false",
			annotationIngressExposed:   "false",
		}),
		svc("not-exact", "landscape", map[string]string{
			annotationDeploymentResult: "not-exact",
			annotationIngressExposed:   "TRUE",
		}),
	}}

	results, err := u.mapServicesToDeploymentResult(list)
	if err != nil {
		t.Fatalf("mapServicesToDeploymentResult: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results: got %d, want 3", len(results))
	}
	for _, result := range results {
		var spec deploymentresult.ServiceSpec
		if err := json.Unmarshal(result.Spec.Raw, &spec); err != nil {
			t.Fatalf("unmarshal spec for %q: %v", result.Name, err)
		}
		if spec.IngressExposed {
			t.Errorf("IngressExposed for %q: got true, want false", result.Name)
		}

		var rawSpec map[string]any
		if err := json.Unmarshal(result.Spec.Raw, &rawSpec); err != nil {
			t.Fatalf("unmarshal raw spec for %q: %v", result.Name, err)
		}
		if value, ok := rawSpec["IngressExposed"]; !ok || value != false {
			t.Errorf("serialized IngressExposed for %q: got %v (present=%t), want false", result.Name, value, ok)
		}
	}
}

// TestMapServicesToDeploymentResult_MultiplePerDeployment: a bundle exposing several Services yields several results,
// one per opted-in Service (multi-DR per ArtifactDeployment).
func TestMapServicesToDeploymentResult_MultiplePerDeployment(t *testing.T) {
	u := &DeploymentResultStatusUpdater{}
	list := &corev1.ServiceList{Items: []corev1.Service{
		svc("storefront-a1b2", "shop", map[string]string{annotationDeploymentResult: "storefront"}, corev1.ServicePort{Port: 80}),
		svc("storefront-admin-a1b2", "shop", map[string]string{annotationDeploymentResult: "storefront-admin"}, corev1.ServicePort{Port: 8080}),
	}}

	results, err := u.mapServicesToDeploymentResult(list)
	if err != nil {
		t.Fatalf("mapServicesToDeploymentResult: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results: got %d, want 2", len(results))
	}
	names := map[string]bool{results[0].Name: true, results[1].Name: true}
	if !names["storefront"] || !names["storefront-admin"] {
		t.Errorf("result names: got %v", names)
	}
}

// TestMapServicesToDeploymentResult_RejectsDuplicateNameType: two Services opting in with the same annotation value
// yield the same (name, type) pair, which is rejected.
func TestMapServicesToDeploymentResult_RejectsDuplicateNameType(t *testing.T) {
	u := &DeploymentResultStatusUpdater{}
	list := &corev1.ServiceList{Items: []corev1.Service{
		svc("candidates-a1", "shop", map[string]string{annotationDeploymentResult: candidatesResult}, corev1.ServicePort{Port: 80}),
		svc("candidates-b2", "shop", map[string]string{annotationDeploymentResult: candidatesResult}, corev1.ServicePort{Port: 80}),
	}}

	if _, err := u.mapServicesToDeploymentResult(list); err == nil {
		t.Fatal("expected error for duplicate (name, type), got nil")
	}
}
