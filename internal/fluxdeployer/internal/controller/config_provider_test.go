package controller

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
)

var _ = Describe("ConfigProvider", func() {
	var (
		ctx context.Context
		cp  *ConfigProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		cp = &ConfigProvider{Client: k8sClient}
	})

	Context("GetKubeConfigRef from configmap", func() {
		const (
			systemNS   = "konfidence-system"
			configName = "flux-deployer-configuration"
			landscape  = "prod"
			otherLand  = "other"
		)

		BeforeEach(func() {
			// ensure namespace exists
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: systemNS}}
			_ = k8sClient.Create(ctx, ns) // ignore error if already exists

			dt := []DeploymentTarget{
				{
					Landscape: landscape,
					SecretRef: &fluxmeta.SecretKeyReference{Name: "remote-secret", Key: "kubeconfig"},
				},
				{
					Landscape:    otherLand,
					ConfigMapRef: &fluxmeta.LocalObjectReference{Name: "other-cm"},
				},
			}
			raw, err := json.Marshal(dt)
			Expect(err).NotTo(HaveOccurred())

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configName, Namespace: systemNS},
				Data:       map[string]string{"deploymentTargets": string(raw)},
			}
			// Create or Update to be resilient across tests
			err = k8sClient.Create(ctx, cm)
			if err != nil {
				// try update if already exists
				existing := &corev1.ConfigMap{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: systemNS, Name: configName}, existing)).To(Succeed())
				existing.Data = cm.Data
				Expect(k8sClient.Update(ctx, existing)).To(Succeed())
			}
		})

		It("returns kubeconfig reference from the global configmap when matching landscape is present", func() {
			ref, err := cp.GetKubeConfigRef(ctx, landscape)
			Expect(err).NotTo(HaveOccurred())
			Expect(ref).NotTo(BeNil())
			Expect(ref.SecretRef).NotTo(BeNil())
			Expect(ref.SecretRef.Name).To(Equal("remote-secret"))
			Expect(ref.SecretRef.Key).To(Equal("kubeconfig"))
			Expect(ref.ConfigMapRef).To(BeNil())
		})
	})

	Context("GetKubeConfigRef from namespace secret", func() {
		const (
			landscapeNS = "dev"
			secretName  = "konfidence-flux-remote-cluster-kubeconfig"
		)

		BeforeEach(func() {
			// ensure namespace exists
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: landscapeNS}}
			_ = k8sClient.Create(ctx, ns)

			// create the conventional secret
			s := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: landscapeNS},
				Type:       corev1.SecretTypeOpaque,
				Data:       map[string][]byte{"kubeconfig": []byte("dummy")},
			}
			// Create or update
			err := k8sClient.Create(ctx, s)
			if err != nil {
				existing := &corev1.Secret{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: landscapeNS, Name: secretName}, existing)).To(Succeed())
				existing.Data = s.Data
				Expect(k8sClient.Update(ctx, existing)).To(Succeed())
			}
		})

		It("returns kubeconfig reference from namespace secret when configmap doesn't have entry", func() {
			// ensure global configmap either absent or without matching entry
			ref, err := cp.GetKubeConfigRef(ctx, landscapeNS)
			Expect(err).NotTo(HaveOccurred())
			Expect(ref).NotTo(BeNil())
			Expect(ref.SecretRef).NotTo(BeNil())
			Expect(ref.SecretRef.Name).To(Equal(secretName))
			// Key is intentionally empty to use flux default
			Expect(ref.SecretRef.Key).To(BeEmpty())
			Expect(ref.ConfigMapRef).To(BeNil())
		})
	})

	Context("GetKubeConfigRef with no targets", func() {
		It("returns nil when neither configmap nor secret are present", func() {
			ref, err := cp.GetKubeConfigRef(ctx, "nonexistent")
			Expect(err).NotTo(HaveOccurred())
			Expect(ref).To(BeNil())
		})
	})

	It("GetTargetNamespace echoes landscape", func() {
		Expect(cp.GetTargetNamespace("staging")).To(Equal("staging"))
	})
})
