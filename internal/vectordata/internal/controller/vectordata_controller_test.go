package controller

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("VectorData Controller", func() {
	const (
		ns                 = "default"
		timeout            = 10 * time.Second
		interval           = 250 * time.Millisecond
		httpK8sServiceType = "http-k8s-service"
		apiResult          = "api"
	)

	AfterEach(func() {
		vdList := &konfidencev1alpha1.VectorDataList{}
		Expect(k8sClient.List(context.Background(), vdList)).To(Succeed())
		for i := range vdList.Items {
			_ = k8sClient.Delete(context.Background(), &vdList.Items[i])
		}
		Eventually(func() int {
			l := &konfidencev1alpha1.VectorDataList{}
			_ = k8sClient.List(context.Background(), l)
			return len(l.Items)
		}, timeout, interval).Should(Equal(0))
		cmList := &corev1.ConfigMapList{}
		Expect(k8sClient.List(context.Background(), cmList)).To(Succeed())
		for i := range cmList.Items {
			cm := &cmList.Items[i]
			if _, ok := cm.Labels[labelManagedBy]; ok {
				_ = k8sClient.Delete(context.Background(), cm)
			}
		}
	})

	Context("Happy path", func() {
		It("materializes a ConfigMap and reports Ready=True", func() {
			vd := newVectorData("vd-happy",
				`{"betaApi":false,"darkMode":true}`,
				`{"_origin":"test"}`,
				map[string]konfidencev1alpha1.ComponentDeploymentResults{
					"github.com/example/component-a": {
						{Name: "endpoint", Type: "serviceEndpoint", Spec: runtime.RawExtension{Raw: []byte(`{"url":"http://a"}`)}},
					},
				})
			Expect(k8sClient.Create(context.Background(), vd)).To(Succeed())

			cmKey := types.NamespacedName{Namespace: ns, Name: ConfigMapPrefix + "vd-happy"}
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(context.Background(), cmKey, cm)).To(Succeed())
				g.Expect(cm.Data).To(HaveKey(FeaturesConfigKey))
				g.Expect(cm.Data).To(HaveKey(AuthoredConfigKey))
				g.Expect(cm.Data).To(HaveKey(DeploymentResultsPrefix + "component-a" + JSONSuffix))
				var results []konfidencev1alpha1.DeploymentResult
				g.Expect(json.Unmarshal([]byte(cm.Data[DeploymentResultsPrefix+"component-a"+JSONSuffix]), &results)).To(Succeed())
				g.Expect(results).To(HaveLen(1))
				g.Expect(results[0].Name).To(Equal("endpoint"))
				g.Expect(cm.Immutable).ToNot(BeNil())
				g.Expect(*cm.Immutable).To(BeTrue())
				g.Expect(cm.OwnerReferences).To(BeEmpty(), "ConfigMap must not have owner-references")
				g.Expect(cm.Labels).To(HaveKeyWithValue(labelVectorDataName, "vd-happy"))
				g.Expect(cm.Labels).To(HaveKeyWithValue(labelVectorDataUID, string(vd.UID)))
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				got := &konfidencev1alpha1.VectorData{}
				g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "vd-happy"}, got)).To(Succeed())
				g.Expect(got.Finalizers).To(ContainElement(VectorDataFinalizer))
				g.Expect(readyCondition(got)).ToNot(BeNil())
				g.Expect(readyCondition(got).Status).To(Equal(metav1.ConditionTrue))
				g.Expect(readyCondition(got).Reason).To(Equal(konfidencev1alpha1.VectorDataReasonMaterialized))
			}, timeout, interval).Should(Succeed())
		})

		It("emits one array entry per Service when a component exposes several", func() {
			vd := newVectorData("vd-multi", `{"a":1}`, `null`,
				map[string]konfidencev1alpha1.ComponentDeploymentResults{
					"github.com/acme/shop/storefront": {
						{Name: "storefront", Type: httpK8sServiceType, Spec: runtime.RawExtension{Raw: []byte(`{"K8sName":"storefront-a1b2"}`)}},
						{Name: "storefront-admin", Type: httpK8sServiceType, Spec: runtime.RawExtension{Raw: []byte(`{"K8sName":"storefront-admin-a1b2"}`)}},
					},
				})
			Expect(k8sClient.Create(context.Background(), vd)).To(Succeed())

			cmKey := types.NamespacedName{Namespace: ns, Name: ConfigMapPrefix + "vd-multi"}
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(context.Background(), cmKey, cm)).To(Succeed())
				g.Expect(cm.Data).To(HaveKey(DeploymentResultsPrefix + "storefront" + JSONSuffix))
				var results []konfidencev1alpha1.DeploymentResult
				g.Expect(json.Unmarshal([]byte(cm.Data[DeploymentResultsPrefix+"storefront"+JSONSuffix]), &results)).To(Succeed())
				g.Expect(results).To(HaveLen(2))
				g.Expect(results[0].Name).To(Equal("storefront"))
				g.Expect(results[1].Name).To(Equal("storefront-admin"))
			}, timeout, interval).Should(Succeed())
		})

		It("deletes the ConfigMap on VectorData deletion via the finalizer", func() {
			vd := newVectorData("vd-delete", `{"a":1}`, `null`, nil)
			Expect(k8sClient.Create(context.Background(), vd)).To(Succeed())

			cmKey := types.NamespacedName{Namespace: ns, Name: ConfigMapPrefix + "vd-delete"}
			Eventually(func() error {
				return k8sClient.Get(context.Background(), cmKey, &corev1.ConfigMap{})
			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Delete(context.Background(), vd)).To(Succeed())

			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(context.Background(), cmKey, &corev1.ConfigMap{}))
			}, timeout, interval).Should(BeTrue())
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(context.Background(),
					types.NamespacedName{Namespace: ns, Name: "vd-delete"}, &konfidencev1alpha1.VectorData{}))
			}, timeout, interval).Should(BeTrue())
		})

		It("is a no-op on a subsequent reconcile when the ConfigMap already exists (immutable)", func() {
			vd := newVectorData("vd-idempotent", `{"a":1}`, `null`, nil)
			Expect(k8sClient.Create(context.Background(), vd)).To(Succeed())

			cmKey := types.NamespacedName{Namespace: ns, Name: ConfigMapPrefix + "vd-idempotent"}
			cm := &corev1.ConfigMap{}
			Eventually(func() error { return k8sClient.Get(context.Background(), cmKey, cm) }, timeout, interval).Should(Succeed())
			firstUID := cm.UID
			firstRV := cm.ResourceVersion

			// Trigger another reconcile via a metadata change (annotation).
			got := &konfidencev1alpha1.VectorData{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "vd-idempotent"}, got)).To(Succeed())
			if got.Annotations == nil {
				got.Annotations = map[string]string{}
			}
			got.Annotations["poke"] = "1"
			Expect(k8sClient.Update(context.Background(), got)).To(Succeed())

			// The ConfigMap must be the same object (same UID, unchanged ResourceVersion).
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(context.Background(), cmKey, cm)).To(Succeed())
				g.Expect(cm.UID).To(Equal(firstUID))
				g.Expect(cm.ResourceVersion).To(Equal(firstRV))
			}, 2*time.Second, interval).Should(Succeed())
		})
	})

	Context("Rendering failures", func() {
		It("reports Ready=False when two components collide on the same result key", func() {
			vd := newVectorData("vd-collision", `{"a":1}`, `null`,
				map[string]konfidencev1alpha1.ComponentDeploymentResults{
					"github.com/acme/team-a/api": {{Name: apiResult, Type: httpK8sServiceType, Spec: runtime.RawExtension{Raw: []byte(`{"K8sName":"api-a"}`)}}},
					"github.com/acme/team-b/api": {{Name: apiResult, Type: httpK8sServiceType, Spec: runtime.RawExtension{Raw: []byte(`{"K8sName":"api-b"}`)}}},
				})
			Expect(k8sClient.Create(context.Background(), vd)).To(Succeed())

			Eventually(func(g Gomega) {
				got := &konfidencev1alpha1.VectorData{}
				g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "vd-collision"}, got)).To(Succeed())
				g.Expect(readyCondition(got)).ToNot(BeNil())
				g.Expect(readyCondition(got).Status).To(Equal(metav1.ConditionFalse))
				g.Expect(readyCondition(got).Reason).To(Equal("RenderFailed"))
			}, timeout, interval).Should(Succeed())

			Consistently(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(context.Background(),
					types.NamespacedName{Namespace: ns, Name: ConfigMapPrefix + "vd-collision"}, &corev1.ConfigMap{}))
			}, 2*time.Second, interval).Should(BeTrue())
		})

		It("rejects a VectorData whose component results collide on (name, type)", func() {
			vd := newVectorData("vd-dup-result", `{"a":1}`, `null`,
				map[string]konfidencev1alpha1.ComponentDeploymentResults{
					"github.com/acme/team-a/api": {
						{Name: apiResult, Type: httpK8sServiceType, Spec: runtime.RawExtension{Raw: []byte(`{"K8sName":"api-1"}`)}},
						{Name: apiResult, Type: httpK8sServiceType, Spec: runtime.RawExtension{Raw: []byte(`{"K8sName":"api-2"}`)}},
					},
				})
			// The CRD's CEL rule enforces (name, type) uniqueness, so the apiserver rejects this at admission.
			Expect(k8sClient.Create(context.Background(), vd)).ToNot(Succeed())
		})
	})
})

func newVectorData(
	name, featuresJSON, authoredJSON string,
	results map[string]konfidencev1alpha1.ComponentDeploymentResults,
) *konfidencev1alpha1.VectorData {
	spec := konfidencev1alpha1.VectorDataSpec{DeploymentResults: results}
	if featuresJSON != "" {
		spec.Features = &runtime.RawExtension{Raw: json.RawMessage(featuresJSON)}
	}
	if authoredJSON != "" {
		spec.Authored = &runtime.RawExtension{Raw: json.RawMessage(authoredJSON)}
	}
	return &konfidencev1alpha1.VectorData{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       spec,
	}
}

func readyCondition(vd *konfidencev1alpha1.VectorData) *metav1.Condition {
	for i := range vd.Status.Conditions {
		if vd.Status.Conditions[i].Type == "Ready" {
			return &vd.Status.Conditions[i]
		}
	}
	return nil
}
