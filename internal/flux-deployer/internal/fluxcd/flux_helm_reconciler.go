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

package fluxcd

import (
	"context"

	landscapev1alpha1 "github.com/konfidence-project/konfidence/api/star/v1alpha1"
)

// FluxHelmReconciler defines the interface for reconciling helm related types of Flux resources
// based on ArtifactDeployment specifications.
type FluxHelmReconciler interface {

	// Reconcile creates or updates the appropriate Flux resources (HelmRelease, etc.)
	// based on the provided ArtifactDeployment and OCMResource.
	Reconcile(ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, helmChartResource *HelmChartResource) (isReady bool, err error)
}

type HelmChartResource struct {
	landscapev1alpha1.OCMResource
	Repository string
	ChartName  string
	Version    string
}
