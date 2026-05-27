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

package cmd

import (
	"os"

	// fluxcd schemes
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	// konfidence CRDs
	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

var (
	enableLeaderElection bool
	probeAddr            string
	controllersSpec      string
	leaseID              string
)

var rootCmd = &cobra.Command{
	Use:   "kubernetes-landscape-orchestrator",
	Short: "Kubernetes landscape orchestrator operator",
	Long: `kubernetes-landscape-orchestrator runs the landscape operator controllers.
By default all controllers are started. Use --controllers to select a subset.`,
	RunE: startOperator,
}

// Execute is the entry point called by main.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(landscapev1alpha1.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
	utilruntime.Must(helmv2.AddToScheme(scheme))
	utilruntime.Must(kustomizev1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.Install(scheme))

	loggerOpts := zap.Options{
		Development: true,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&loggerOpts)))

	rootCmd.PersistentFlags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	rootCmd.PersistentFlags().BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	rootCmd.Flags().StringVar(&controllersSpec, "controllers", "*",
		"Comma-separated glob expression selecting which controllers to enable. "+
			"Examples: '*' (all), 'flux-deployer', '!flux-deployer,*' (all except), 'task-*'. "+
			"Tokens are set-based and order-independent; '!' negates.")

	rootCmd.Flags().StringVar(&leaseID, "lease-id", "orchestrator.konfidence.cloud",
		"The ID used for leader election.")
}
