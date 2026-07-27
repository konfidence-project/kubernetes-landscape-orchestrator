package fluxcd_test

import (
	"testing"

	"github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
)

const (
	helmChartType     = "helmChart"
	kustomizeType     = "kustomize"
	ociArtifactType   = "ociArtifact"
	imageReferenceKey = "imageReference"
	podinfoHelmName   = "podinfo-helm"
	contentTypeKey    = "type"
)

func TestToHelm_OCIArtifact(t *testing.T) {
	res := v1alpha1.OCMResource{
		Name: podinfoHelmName,
		Type: helmChartType,
		Content: rawJSON(t, map[string]interface{}{
			contentTypeKey:    ociArtifactType,
			imageReferenceKey: "host.docker.internal:5000/helm-charts/podinfo:6.9.1",
		}),
	}

	h, err := fluxcd.Map(res).ToHelm()
	require.NoError(t, err)

	require.Equal(t, "oci://host.docker.internal:5000/helm-charts", h.Repository)
	require.Equal(t, "podinfo", h.ChartName)
	require.Equal(t, "6.9.1", h.Version)
}

func TestToHelm_HelmRepository(t *testing.T) {
	res := v1alpha1.OCMResource{
		Name: podinfoHelmName,
		Type: helmChartType,
		Content: rawJSON(t, map[string]interface{}{
			contentTypeKey:   "helm",
			"helmChart":      "podinfo:6.9.1",
			"helmRepository": "https://stefanprodan.github.io/podinfo",
		}),
	}

	h, err := fluxcd.Map(res).ToHelm()
	require.NoError(t, err)

	require.Equal(t, "https://stefanprodan.github.io/podinfo", h.Repository)
	require.Equal(t, "podinfo", h.ChartName)
	require.Equal(t, "6.9.1", h.Version)
}

func TestToHelm_HelmRepository_SeparateFields(t *testing.T) {
	res := v1alpha1.OCMResource{
		Name: podinfoHelmName,
		Type: helmChartType,
		Content: rawJSON(t, map[string]interface{}{
			contentTypeKey:   "helm",
			"helmChart":      "podinfo",
			"version":        "6.9.1",
			"helmRepository": "https://repo",
		}),
	}

	h, err := fluxcd.Map(res).ToHelm()
	require.NoError(t, err)

	require.Equal(t, "podinfo", h.ChartName)
	require.Equal(t, "6.9.1", h.Version)
}

func TestToHelm_InvalidType(t *testing.T) {
	res := v1alpha1.OCMResource{
		Type: kustomizeType,
	}

	_, err := fluxcd.Map(res).ToHelm()
	require.Error(t, err)
}

func TestToHelm_InvalidOCIReference(t *testing.T) {
	res := v1alpha1.OCMResource{
		Type: helmChartType,
		Content: rawJSON(t, map[string]interface{}{
			contentTypeKey:    ociArtifactType,
			imageReferenceKey: "not-a-valid-ref",
		}),
	}

	_, err := fluxcd.Map(res).ToHelm()
	require.Error(t, err)
}

func TestToKustomize_OCIArtifact(t *testing.T) {
	res := v1alpha1.OCMResource{
		Name: "podinfo-kustomize",
		Type: kustomizeType,
		Content: rawJSON(t, map[string]interface{}{
			contentTypeKey:    ociArtifactType,
			imageReferenceKey: "demo.goharbor.io/kden-test/kustomize/podinfo:v0.1.0",
		}),
	}

	k, err := fluxcd.Map(res).ToKustomize()
	require.NoError(t, err)

	require.Equal(t, "./", k.Path)
	require.Equal(t, "v0.1.0", k.Tag)
	require.Equal(t, "oci://demo.goharbor.io/kden-test/kustomize/podinfo", k.Repository)
}

func TestToKustomize_InvalidType(t *testing.T) {
	res := v1alpha1.OCMResource{
		Type: helmChartType,
	}

	_, err := fluxcd.Map(res).ToKustomize()
	require.Error(t, err)
}

func TestToKustomize_InvalidOCIReference(t *testing.T) {
	res := v1alpha1.OCMResource{
		Type: kustomizeType,
		Content: rawJSON(t, map[string]interface{}{
			contentTypeKey:    ociArtifactType,
			imageReferenceKey: "invalid-ref",
		}),
	}

	_, err := fluxcd.Map(res).ToKustomize()
	require.Error(t, err)
}

func rawJSON(t *testing.T, obj interface{}) runtime.RawExtension {
	data, err := json.Marshal(obj)
	require.NoError(t, err)
	return runtime.RawExtension{Raw: data}
}
