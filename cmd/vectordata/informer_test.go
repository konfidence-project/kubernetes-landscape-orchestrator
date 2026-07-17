package main

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	toolscache "k8s.io/client-go/tools/cache"
)

var _ = Describe("Vector Data Informer", func() {
	It("invalidates the cache entry keyed by vectorId when the ConfigMap is deleted", func() {
		cache := &InMemoryCache{store: map[string]string{VectorId: VectorConfig}}
		configMap := getDefaultConfigMap()
		fakeClient := fake.NewSimpleClientset(configMap)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		errChan := make(chan error, 1)

		informer := NewInformer(cache, corev1.NamespaceDefault).setupAndStart(ctx, fakeClient, errChan)
		Expect(toolscache.WaitForCacheSync(ctx.Done(), informer.HasSynced)).To(BeTrue())

		err := fakeClient.CoreV1().ConfigMaps(corev1.NamespaceDefault).Delete(ctx, configMap.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() bool {
			_, ok := cache.Get(VectorId)
			return ok
		}).Should(BeFalse())

		Consistently(errChan).ShouldNot(Receive())
	})
})
