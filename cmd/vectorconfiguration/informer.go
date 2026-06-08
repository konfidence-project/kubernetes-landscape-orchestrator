package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type Informer struct {
	Cache     Cache
	Namespace string
}

// NewInformer returns an initialized configuration service instance
func NewInformer(flagCache Cache, namespace string) *Informer {
	return &Informer{Cache: flagCache, Namespace: namespace}
}

func (i *Informer) setupAndStart(ctx context.Context, watcherClient *kubernetes.Clientset, errChan chan error) {
	informerFactory := informers.NewSharedInformerFactoryWithOptions(watcherClient, 10*time.Minute, informers.WithNamespace(i.Namespace))
	configMapInformer := informerFactory.Core().V1().ConfigMaps().Informer()
	_, err := configMapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldConfigMap := oldObj.(*corev1.ConfigMap)
			newConfigMap := newObj.(*corev1.ConfigMap)

			// do not process if actual payload did not change
			if oldConfigMap.ResourceVersion == newConfigMap.ResourceVersion {
				return
			}

			slog.Info(fmt.Sprintf("ConfigMap updated %s/%s. Removing entry from cache if necessary...", newConfigMap.Namespace, newConfigMap.Name))
			i.Cache.Set(newConfigMap.Name, "")
		},
		DeleteFunc: func(obj interface{}) {
			var configMap *corev1.ConfigMap
			var ok bool
			if configMap, ok = obj.(*corev1.ConfigMap); !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					slog.Error("missed configMap delete, decoding failed")
					return
				}
				if configMap, ok = tombstone.Obj.(*corev1.ConfigMap); !ok {
					slog.Error("tombstone contained invalid object")
					return
				}
			}

			slog.Info(fmt.Sprintf("ConfigMap deleted %s/%s. Removing entry from cache if necessary...", configMap.Namespace, configMap.Name))
			i.Cache.Set(configMap.Name, "")
		},
	})

	if err != nil {
		slog.Error(fmt.Sprintf("could not initialize configMap informer: %v", err))
		os.Exit(1)
	}

	informerFactory.Start(ctx.Done())
	go func() {
		slog.Info(fmt.Sprintf("Syncing cache for namespace: %s", i.Namespace))
		syncCtx, syncCancel := context.WithTimeout(ctx, 30*time.Second)
		defer syncCancel()

		if !cache.WaitForCacheSync(syncCtx.Done(), configMapInformer.HasSynced) {
			if ctx.Err() != nil {
				slog.Info("Cache sync canceled due to application shutdown.")
				return
			}

			if errors.Is(syncCtx.Err(), context.DeadlineExceeded) {
				errChan <- errors.New("kubernetes informer cache failed to sync within timeout")
				return
			}

			errChan <- fmt.Errorf("kubernetes informer cache failed to sync: %w", syncCtx.Err())
		} else {
			slog.Info(fmt.Sprintf("Watching for ConfigMap updates/deletions inside namespace: %s", i.Namespace))
		}
	}()
}
