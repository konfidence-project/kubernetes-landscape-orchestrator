package controller

import (
	"testing"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
)

// renderData's guard is defence-in-depth for the (name, type) uniqueness the CRD's CEL rule already enforces at
// admission; this exercises it directly, without the apiserver.
func TestRenderData_RejectsDuplicateResultsWithinComponent(t *testing.T) {
	vd := &konfidencev1alpha1.VectorData{
		Spec: konfidencev1alpha1.VectorDataSpec{
			DeploymentResults: map[string]konfidencev1alpha1.ComponentDeploymentResults{
				"github.com/acme/svc": {
					{Name: "api", Type: "http-k8s-service", Spec: runtime.RawExtension{Raw: []byte(`{"K8sName":"api-1"}`)}},
					{Name: "api", Type: "http-k8s-service", Spec: runtime.RawExtension{Raw: []byte(`{"K8sName":"api-2"}`)}},
				},
			},
		},
	}
	if _, err := renderData(vd); err == nil {
		t.Fatal("expected error for duplicate (name, type) within a component, got nil")
	}
}
