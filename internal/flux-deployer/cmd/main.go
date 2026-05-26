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

package main

import (
	"flag"
	"os"

	"github.com/konfidence-project/landscape-flux-deployer/internal/fluxcd/reconciler"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	// see https://github.com/fluxcd/helm-controller/tree/main/api/v2
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	// see https://github.com/fluxcd/kustomize-controller/tree/main/api/v1
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"

	// see https://github.com/konfidence-project/crds/tree/main/api/landscape/v1alpha1
	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	"github.com/konfidence-project/landscape-flux-deployer/internal/controller"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(landscapev1alpha1.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
	utilruntime.Must(helmv2.AddToScheme(scheme))
	utilruntime.Must(kustomizev1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.Install(scheme))

	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "8fd85f93.konfidence.cloud",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	configProvider := &controller.ConfigProvider{Client: mgr.GetClient()}

	if err := (&controller.HelmArtifactDeploymentReconciler{
		Client:                        mgr.GetClient(),
		ReadyConditionStatusUpdater:   &controller.ReadyConditionStatusUpdater{},
		DeploymentResultStatusUpdater: &controller.DeploymentResultStatusUpdater{Client: mgr.GetClient()},
		HelmRepositoryReconciler: &reconciler.HelmRepositoryReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), ConfigProvider: configProvider,
			Recorder: mgr.GetEventRecorder(reconciler.HelmRepositoryControllerName)},
		HelmReleaseReconciler: &reconciler.HelmReleaseReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), ConfigProvider: configProvider},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create helm controller", "controller", "ArtifactDeployment")
		os.Exit(1)
	}
	if err := (&controller.KustomizeArtifactDeploymentReconciler{
		Client:                        mgr.GetClient(),
		ReadyConditionStatusUpdater:   &controller.ReadyConditionStatusUpdater{},
		DeploymentResultStatusUpdater: &controller.DeploymentResultStatusUpdater{Client: mgr.GetClient()},
		OCIRepositoryReconciler: &reconciler.OCIRepositoryReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), ConfigProvider: configProvider,
			Recorder: mgr.GetEventRecorder(reconciler.OCIRepositoryControllerName)},
		KustomizationReconciler: &reconciler.KustomizationReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), ConfigProvider: configProvider,
			Recorder: mgr.GetEventRecorder(reconciler.KustomizationControllerName)},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create kustomize controller", "controller", "ArtifactDeployment")
		os.Exit(1)
	}
	if err := (&controller.VectorAssignmentReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorder(controller.VectorAssignmentControllerName),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create vector assignment controller", "controller", "VectorAssignment")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
