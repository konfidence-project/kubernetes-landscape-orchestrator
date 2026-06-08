package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	VectorConfigServicePortEnv      = "VECTOR_CONFIGURATION_SERVICE_PORT"
	VectorConfigServiceNamespaceEnv = "VECTOR_CONFIGURATION_SERVICE_NAMESPACE"
	DefaultPort                     = 4000
)

func main() {
	port := initServerPort()
	namespace := os.Getenv(VectorConfigServiceNamespaceEnv)
	if namespace == "" {
		slog.Info(fmt.Sprintf("No namespace configured in environment variable %s, using default namespace", VectorConfigServiceNamespaceEnv))
		namespace = "default"
	}

	slog.Info(fmt.Sprintf("Using namespace %s", namespace))
	slog.Info("Get k8s configuration...")
	config := getK8sConfig()

	slog.Info("Initializing k8s api client...")
	k8sClient := createClientSet(config)

	slog.Info("Initializing k8s watcher client...")
	watcherClient := createClientSet(config)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errChan := make(chan error, 2)

	cache := &InMemoryCache{store: make(map[string]string)}
	NewInformer(cache, namespace).setupAndStart(ctx, watcherClient, errChan)
	server := NewConfigurationService(k8sClient, cache, namespace, port).Start(errChan)

	select {
	case <-ctx.Done():
		slog.Info("System interrupt signal intercepted.")
	case backgroundErr := <-errChan:
		slog.Error(fmt.Sprintf("application error occurred: %v", backgroundErr))
	}

	slog.Info("Stopping processes and cleaning connections...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error(fmt.Sprintf("error shutting down server: %v", err))
	}

	slog.Info("Application completely exited.")

	// if we exited due to a background error, ensure we exit with status 1 so k8s restarts the pod
	if len(errChan) > 0 {
		os.Exit(1)
	}
}

func initServerPort() int {
	var portFlag int
	flag.IntVar(&portFlag, "port", DefaultPort, "Server port")
	flag.Parse()

	port := portFlag
	if envPort := os.Getenv(VectorConfigServicePortEnv); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			port = p
		}
	}

	slog.Info(fmt.Sprintf("Using port: %d", port))
	return port
}

func getK8sConfig() *rest.Config {
	var config *rest.Config
	var err error
	slog.Info("Loading In-Cluster config...")

	// try In-Cluster config (if app runs in pod)
	config, err = rest.InClusterConfig()
	if err != nil {
		// fallback: local kubeconfig
		slog.Info("In-Cluster config not available. Trying to use local config as fallback...")
		kubeconfig := clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			slog.Error(fmt.Sprintf("could not load kubernetes config: %v", err))
			os.Exit(1)
		}
	}

	return config
}

func createClientSet(config *rest.Config) *kubernetes.Clientset {
	slog.Info("Successfully loaded kubernetes configuration")
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		slog.Error(fmt.Sprintf("could not create k8s client: %v", err))
		os.Exit(1)
	}

	slog.Info("Successfully created k8s client")
	return k8sClient
}
