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
	"context"

	fluxcontroller "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/flux-deployer/controller"
	activationcontroller "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/kubernetes-activation-execution/controller"
	taskcontroller "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/kubernetes-task-execution/controller"
	utilscmd "github.com/konfidence-project/kubernetes-landscape-orchestrator/utils/cmd"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

func startOperator(cmd *cobra.Command, args []string) error {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       leaseID,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	signalContext, cancel := context.WithCancel(ctrl.SetupSignalHandler())
	defer cancel()

	controllerSetups := map[string]func() error{
		fluxcontroller.OperatorFlagName: func() error {
			return fluxcontroller.SetupControllers(mgr, setupLog)
		},
		taskcontroller.OperatorFlagName: func() error {
			return taskcontroller.SetupControllers(mgr, setupLog)
		},
		activationcontroller.OperatorFlagName: func() error {
			return activationcontroller.SetupControllers(mgr, setupLog)
		},
	}

	enabled, err := utilscmd.FilterEnabledControllers(controllersSpec, controllerSetups)
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
