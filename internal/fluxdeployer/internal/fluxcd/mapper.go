package fluxcd

import (
	"fmt"
	"path"
	"strings"

	"github.com/distribution/reference"
	"github.com/konfidence-project/konfidence/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/json"
)

type mapper struct {
	resource v1alpha1.OCMResource
}

func (m mapper) ToHelm() (*HelmChartResource, error) {
	if m.resource.Type != "helmChart" {
		return nil, fmt.Errorf("type '%s' not parsable to HelmChartResource", m.resource.Type)
	}

	var accessType struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(m.resource.Content.Raw, &accessType); err != nil {
		return nil, fmt.Errorf("unable to unmarshal content to accessType wrapper: %w", err)
	}

	switch accessType.Type {
	case "helm":
		var helmContent struct {
			Type           string `json:"type"`
			HelmRepository string `json:"helmRepository"`
			HelmChart      string `json:"helmChart"`
			Version        string `json:"version"`
		}

		if err := json.Unmarshal(m.resource.Content.Raw, &helmContent); err != nil {
			return nil, fmt.Errorf("unable to unmarshal content to HelmChartResource: %w", err)
		}

		helmChartResource := &HelmChartResource{
			OCMResource: m.resource,
			Repository:  helmContent.HelmRepository,
		}

		if strings.Contains(helmContent.HelmChart, ":") {
			parts := strings.Split(helmContent.HelmChart, ":")
			helmChartResource.ChartName = parts[0]
			helmChartResource.Version = parts[1]
		} else {
			helmChartResource.ChartName = helmContent.HelmChart
			helmChartResource.Version = helmContent.Version
		}

		return helmChartResource, nil
	case "ociArtifact":
		var ociContent struct {
			Type           string `json:"type"`
			ImageReference string `json:"imageReference"`
		}

		if err := json.Unmarshal(m.resource.Content.Raw, &ociContent); err != nil {
			return nil, fmt.Errorf("unable to unmarshal content to HelmChartResource: %w", err)
		}

		ref, err := reference.Parse(ociContent.ImageReference)
		if err != nil {
			return nil, fmt.Errorf("unable to parse kustomizeImage reference: %w", err)
		}

		namedTaggedRef, ok := ref.(reference.NamedTagged)
		if !ok {
			return nil, fmt.Errorf("imageReference must be in format %q, got %q", "<repository-url>:<tag>", ociContent.ImageReference)
		}

		return &HelmChartResource{
			OCMResource: m.resource,
			Repository:  fmt.Sprintf("oci://%s/%s", reference.Domain(namedTaggedRef), path.Dir(reference.Path(namedTaggedRef))),
			ChartName:   path.Base(reference.Path(namedTaggedRef)),
			Version:     namedTaggedRef.Tag(),
		}, nil
	default:
		return nil, fmt.Errorf("type '%s' not parsable to HelmChartResource (supported: helm, ociArtifact)", m.resource.Type)
	}
}

func (m mapper) ToKustomize() (*KustomizeResource, error) {
	if m.resource.Type != "kustomize" {
		return nil, fmt.Errorf("type '%s' not parsable to KustomizeResource", m.resource.Type)
	}

	var kustomizeContent struct {
		Type           string `json:"type"`
		ImageReference string `json:"imageReference"`
	}
	if err := json.Unmarshal(m.resource.Content.Raw, &kustomizeContent); err != nil {
		return nil, fmt.Errorf("unable to unmarshal content to KustomizeResource: %w", err)
	}

	if kustomizeContent.Type != "ociArtifact" {
		return nil, fmt.Errorf("type '%s' not parsable to KustomizeResource", kustomizeContent.Type)
	}

	ref, err := reference.Parse(kustomizeContent.ImageReference)
	if err != nil {
		return nil, fmt.Errorf("unable to parse kustomizeImage reference: %w", err)
	}

	namedTaggedRef, ok := ref.(reference.NamedTagged)
	if !ok {
		return nil, fmt.Errorf("imageReference must be in format %q, got %q", "<repository-url>:<tag>", kustomizeContent.ImageReference)
	}

	kustomizeResource := &KustomizeResource{
		OCMResource: m.resource,
		Path:        "./", // TODO use path from OCM (custom access type?)
		Tag:         namedTaggedRef.Tag(),
		Repository:  fmt.Sprintf("oci://%s/%s", reference.Domain(namedTaggedRef), reference.Path(namedTaggedRef)),
	}

	return kustomizeResource, nil
}

func Map(resource v1alpha1.OCMResource) mapper {
	return mapper{resource: resource}
}
