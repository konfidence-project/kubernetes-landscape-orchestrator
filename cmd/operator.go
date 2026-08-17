package cmd

import (
	"context"
	"maps"
	"slices"

	utilscmd "github.com/konfidence-project/konfidence/pkg/cmd"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/activationexecution"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/taskexecution"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/vectordata"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func startOperator(cmd *cobra.Command, args []string) error {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       leaseID,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	signalContext, cancel := context.WithCancel(ctrl.SetupSignalHandler())
	defer cancel()

	controllerSetups := map[string]func() error{
		fluxdeployer.OperatorFlagName: func() error {
			return fluxdeployer.SetupControllers(mgr, setupLog)
		},
		taskexecution.OperatorFlagName: func() error {
			return taskexecution.SetupControllers(mgr, setupLog)
		},
		activationexecution.OperatorFlagName: func() error {
			return activationexecution.SetupControllers(mgr, setupLog)
		},
		vectordata.OperatorFlagName: func() error {
			return vectordata.SetupControllers(mgr, setupLog)
		},
	}

	enabled, err := utilscmd.FilterEnabledControllers(controllersSpec, slices.Collect(maps.Keys(controllerSetups)))
	if err != nil {
		setupLog.Error(err, "invalid --controllers flag")
		return err
	}

	for name, setup := range controllerSetups {
		if !enabled[name] {
			setupLog.Info("controller disabled", "controller", name)
			continue
		}
		setupLog.Info("setting up controller", "controller", name)
		if err := setup(); err != nil {
			setupLog.Error(err, "unable to set up controller", "controller", name)
			return err
		}
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(signalContext); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
