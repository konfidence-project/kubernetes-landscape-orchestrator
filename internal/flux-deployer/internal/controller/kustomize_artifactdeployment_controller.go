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
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	// see https://github.com/fluxcd/kustomize-controller/tree/main/api/v1
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"

	// see https://github.com/konfidence-project/crds/tree/main/api/landscape/v1alpha1
	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	"github.com/konfidence-project/landscape-flux-deployer/internal/fluxcd"
)

// KustomizeArtifactDeploymentReconciler reconciles ArtifactDeployment objects where manifest type is 'Kustomize'
type KustomizeArtifactDeploymentReconciler struct {
	client.Client
	DeploymentResultStatusUpdater StatusUpdater
	ReadyConditionStatusUpdater   StatusUpdater
	OCIRepositoryReconciler       fluxcd.FluxKustomizeReconciler
	KustomizationReconciler       fluxcd.FluxKustomizeReconciler
}

// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=artifactdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=artifactdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=artifactdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=ocirepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kustomize.toolkit.fluxcd.io,resources=kustomizations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *KustomizeArtifactDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("start reconciling Kustomize artifact deployment")

	// get the ArtifactDeployment object
	deployment := &landscapev1alpha1.ArtifactDeployment{}
	if err := r.Get(ctx, req.NamespacedName, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get artifact deployment object: %w", err)
	}

	// preserve original deployment status for patching it later
	originalDeployment := deployment.DeepCopy()
	patch := client.MergeFrom(originalDeployment)

	for _, ocmResource := range deployment.Spec.Component.Resources {
		if ocmResource.Type != "kustomize" {
			// we only handle kustomize resources, skip all other resource types
			continue
		}

		kustomizeResource, err := fluxcd.Map(ocmResource).ToKustomize()
		if err != nil {
			log.Error(err, fmt.Sprintf("failed to map OCM resource %q to KustomizeResource", ocmResource.Name),
				"ArtifactDeployment", deployment)
			continue
		}

		if isReady, err := r.OCIRepositoryReconciler.Reconcile(ctx, deployment, kustomizeResource); err != nil {
			log.Error(err, fmt.Sprintf("failed to reconcile OCIRepository of OCM resource '%s'", ocmResource.Name),
				"ArtifactDeployment", deployment)
		} else {
			if isReady {
				if _, err := r.KustomizationReconciler.Reconcile(ctx, deployment, kustomizeResource); err != nil {
					log.Error(err, fmt.Sprintf("failed to reconcile Kustomization of OCM resource '%s'", ocmResource.Name),
						"ArtifactDeployment", deployment)
				}
			} else {
				log.Info("OCIRepository is not ready, skipping Kustomization reconciliation")
			}
		}
	}

	err := r.DeploymentResultStatusUpdater.MutateStatus(ctx, deployment)
	if err != nil {
		log.Error(err, "failed to handle Kustomize deployment result", "ArtifactDeployment", deployment)
	}

	err = r.ReadyConditionStatusUpdater.MutateStatus(ctx, deployment)
	if err != nil {
		log.Error(err, "failed to mutate status condition to READY ", "ArtifactDeployment", deployment)
	}

	// patch the deployment status updates
	if !reflect.DeepEqual(deployment.Status, originalDeployment.Status) {
		if err := r.Client.Status().Patch(ctx, deployment, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to patch artifact deployment status: %w", err)
		}
	}

	log.Info("finish reconciling Kustomize artifact deployment")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KustomizeArtifactDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create a predicate to filter ...
	manifestTypeFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch obj := obj.(type) {
		case *landscapev1alpha1.ArtifactDeployment:
			// ... for 'Kustomize' manifest types
			return obj.Spec.Manifest.Type == "cloud.konfidence.flux.kustomize"
		case *sourcev1.OCIRepository, *kustomizev1.Kustomization:
			// ... or owned resources
			return true
		default:
			return false
		}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&landscapev1alpha1.ArtifactDeployment{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).WithEventFilter(manifestTypeFilter).
		Owns(&sourcev1.OCIRepository{}).
		Owns(&kustomizev1.Kustomization{}).
		Named("kustomize_artifactdeployment").
		Complete(r)
}
