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

package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	// see https://github.com/fluxcd/helm-controller/tree/main/api/v2
	helmv2 "github.com/fluxcd/helm-controller/api/v2"

	// see https://github.com/konfidence-project/crds/tree/main/api/landscape/v1alpha1
	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	"github.com/konfidence-project/landscape-flux-deployer/internal/fluxcd"
)

// HelmArtifactDeploymentReconciler reconciles ArtifactDeployment objects where manifest type is 'Helm'
type HelmArtifactDeploymentReconciler struct {
	client.Client
	HelmRepositoryReconciler fluxcd.FluxReconciler
	HelmReleaseReconciler    fluxcd.FluxReconciler
}

// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=artifactdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=artifactdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=artifactdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmcharts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete

func (r *HelmArtifactDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("start reconciling Helm artifact deployment")

	// get the ArtifactDeployment object
	deployment := &landscapev1alpha1.ArtifactDeployment{}
	if err := r.Get(ctx, req.NamespacedName, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get artifact deployment object: %w", err)
	}

	// preserve original deployment status for patching it later
	patch := client.MergeFrom(deployment.DeepCopy())

	// reconcile each OCM resource of the deployment
	for _, ocmResource := range deployment.Spec.Component.Resources {
		if ocmResource.Type != "helmChart" {
			// we only handle helm chart, skip all other resource types
			continue
		}

		if _, err := r.HelmRepositoryReconciler.Reconcile(ctx, deployment, &ocmResource); err != nil {
			log.Error(err, fmt.Sprintf("failed to reconcile HelmRepository of OCM resource '%s'", ocmResource.Name),
				"ArtifactDeployment", deployment)
		} else {
			if _, err := r.HelmReleaseReconciler.Reconcile(ctx, deployment, &ocmResource); err != nil {
				log.Error(err, fmt.Sprintf("failed to reconcile HelmRelease of OCM resource '%s'", ocmResource.Name),
					"ArtifactDeployment", deployment)
			}
		}
	}

	// TODO (max # 2025-08-08): How to handle status conditions in case of multiple OCM resources (e.g. multiple Helm charts)?
	// Option 1: collect all status conditions and merge them; if the status for a condition differs, set to Unknown (Reason: "DeploymentPartiallyFailed")
	// Option 2: if the conditions is false for any of the OCM resources, set the status to false (i.e. only true if all are true)
	// Option 3: make the behavior configurable (whether option A or B), e.g. via a field in the ArtifactDeployment spec (allowPartialDeployments: bool)

	// patch the deployment status updates
	if err := r.Client.Status().Patch(ctx, deployment, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to patch artifact deployment status: %w", err)
	}

	log.Info("finish reconciling Helm artifact deployment")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HelmArtifactDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create a predicate to filter ...
	manifestTypeFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch obj := obj.(type) {
		case *landscapev1alpha1.ArtifactDeployment:
			// ... for 'Helm' manifest types
			return obj.Spec.Manifest.Type == "cloud.konfidence.flux.helm"
		case *sourcev1.HelmRepository, *sourcev1.HelmChart, *helmv2.HelmRelease:
			// ... or owned resources
			return true
		default:
			return false
		}
	})

	return ctrl.NewControllerManagedBy(mgr).
		Named("helm_artifactdeployment").
		For(&landscapev1alpha1.ArtifactDeployment{}).WithEventFilter(manifestTypeFilter).
		Owns(&sourcev1.HelmRepository{}).
		Owns(&sourcev1.HelmChart{}).
		Owns(&helmv2.HelmRelease{}).
		Complete(r)
}
