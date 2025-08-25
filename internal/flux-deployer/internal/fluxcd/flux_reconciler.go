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

	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"
)

// FluxReconciler defines the interface for reconciling different types of Flux resources
// based on ArtifactDeployment specifications.
type FluxReconciler interface {

	// Reconcile creates or updates the appropriate Flux resources (HelmRelease, Kustomization, etc.)
	// based on the provided ArtifactDeployment and OCMResource.
	Reconcile(ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, ocmResource *landscapev1alpha1.OCMResource) (isReady bool, err error)
}
