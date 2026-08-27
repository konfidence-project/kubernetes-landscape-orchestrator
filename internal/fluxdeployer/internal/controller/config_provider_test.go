package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ConfigProvider", func() {
	const deploymentType = "sample.konfidence.cloud"

	var (
		ctx       context.Context
		cp        *ConfigProvider
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		cp = &ConfigProvider{Client: k8sClient}
		namespace = fmt.Sprintf("config-provider-%d", time.Now().UnixNano())
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})).To(Succeed())
	})

	createReadyTarget := func(name, connectionType, secretName string) {
		var ref *konfidencev1alpha1.ConnectionRef
		if secretName != "" {
			ref = &konfidencev1alpha1.ConnectionRef{Kind: "Secret", Name: secretName}
		}
		target := &konfidencev1alpha1.DeploymentTarget{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: konfidencev1alpha1.DeploymentTargetSpec{
				DeploymentClassName: deploymentType,
				Connection: konfidencev1alpha1.DeploymentTargetConnection{
					Type: connectionType,
					Ref:  ref,
				},
			},
		}
		Expect(k8sClient.Create(ctx, target)).To(Succeed())
		target.Status.Conditions = []metav1.Condition{{
			Type:               konfidencev1alpha1.DeploymentTargetReadyCondition,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: target.Generation,
			Reason:             "Accepted",
			LastTransitionTime: metav1.Now(),
		}}
		Expect(k8sClient.Status().Update(ctx, target)).To(Succeed())
	}

	It("returns the Secret referenced by the ready DeploymentTarget", func() {
		createReadyTarget("remote", "kubeconfig", "remote-kubeconfig")

		ref, err := cp.GetKubeConfigRef(ctx, namespace, deploymentType)
		Expect(err).NotTo(HaveOccurred())
		Expect(ref.SecretRef.Name).To(Equal("remote-kubeconfig"))
	})

	It("returns nil for a local DeploymentTarget", func() {
		createReadyTarget("local", "local", "")

		ref, err := cp.GetKubeConfigRef(ctx, namespace, deploymentType)
		Expect(err).NotTo(HaveOccurred())
		Expect(ref).To(BeNil())
	})

	It("fails when no ready DeploymentTarget exists", func() {
		ref, err := cp.GetKubeConfigRef(ctx, namespace, deploymentType)
		Expect(err).To(MatchError(ContainSubstring("expected exactly one ready DeploymentTarget")))
		Expect(ref).To(BeNil())
	})

	It("fails when multiple ready DeploymentTargets exist", func() {
		createReadyTarget("first", "local", "")
		createReadyTarget("second", "local", "")

		ref, err := cp.GetKubeConfigRef(ctx, namespace, deploymentType)
		Expect(err).To(MatchError(ContainSubstring("found 2")))
		Expect(ref).To(BeNil())
	})

	It("GetTargetNamespace echoes landscape", func() {
		Expect(cp.GetTargetNamespace("staging")).To(Equal("staging"))
	})
})
