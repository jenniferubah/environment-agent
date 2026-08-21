package kubernetes_test

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/dcm-project/environment-agent/api/container/v1alpha1"
	"github.com/dcm-project/environment-agent/internal/openshift/container/store"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("K8s Store", func() {
	Describe("Get Operations", func() {
		// TC-I030: Get returns container with runtime data from Pod and Service
		It("returns container with runtime data from Pod and Service (TC-I030)", func() {
			s, client := newTestStore(defaultConfig())

			// Pre-create Deployment, Pod, and Service
			err := createFakeDeployment(client, "my-app", "abc-123")
			Expect(err).NotTo(HaveOccurred())
			err = createFakePod(client, "my-app-pod", "abc-123", corev1.PodRunning, "10.0.0.1")
			Expect(err).NotTo(HaveOccurred())
			err = createFakeService(client, "default", "my-app", "abc-123", corev1.ServiceTypeClusterIP, []int32{8080}, withClusterIP("10.96.0.1"))
			Expect(err).NotTo(HaveOccurred())

			result, err := s.Get(context.Background(), "abc-123")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			// Status from Pod phase
			Expect(result.Status).NotTo(BeNil())
			Expect(*result.Status).To(Equal(v1alpha1.RUNNING))

			// Network IP from Pod
			Expect(result.Spec.Network).NotTo(BeNil())
			Expect(result.Spec.Network.Ip).NotTo(BeNil())
			Expect(*result.Spec.Network.Ip).To(Equal("10.0.0.1"))

			// Service info
			Expect(result.Service).NotTo(BeNil())
			Expect(result.Service.Name).NotTo(BeNil())
			Expect(*result.Service.Name).To(Equal("my-app"))
			Expect(result.Service.ClusterIp).NotTo(BeNil())
			Expect(*result.Service.ClusterIp).To(Equal("10.96.0.1"))
		})

		// TC-I031: Get returns PENDING status when no Pod exists
		It("returns PENDING status when no Pod exists (TC-I031)", func() {
			s, client := newTestStore(defaultConfig())

			// Pre-create only Deployment, no Pod
			err := createFakeDeployment(client, "my-app", "abc-123")
			Expect(err).NotTo(HaveOccurred())

			result, err := s.Get(context.Background(), "abc-123")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).NotTo(BeNil())
			Expect(*result.Status).To(Equal(v1alpha1.PENDING))
		})

		// TC-I032: Get returns not-found for non-existent container
		It("returns not-found for non-existent container (TC-I032)", func() {
			s, _ := newTestStore(defaultConfig())

			_, err := s.Get(context.Background(), "xyz-999")

			var notFoundErr *store.NotFoundError
			Expect(errors.As(err, &notFoundErr)).To(BeTrue(), "expected NotFoundError, got: %v", err)
		})

		// TC-I033: Get populates externalIP from LoadBalancer status
		It("populates externalIP from LoadBalancer status (TC-I033)", func() {
			s, client := newTestStore(defaultConfig())

			err := createFakeDeployment(client, "my-app", "abc-123")
			Expect(err).NotTo(HaveOccurred())
			err = createFakePod(client, "my-app-pod", "abc-123", corev1.PodRunning, "10.0.0.1")
			Expect(err).NotTo(HaveOccurred())
			err = createFakeService(client, "default", "my-app", "abc-123", corev1.ServiceTypeLoadBalancer, []int32{8080}, withLoadBalancerIP("203.0.113.1"))
			Expect(err).NotTo(HaveOccurred())

			result, err := s.Get(context.Background(), "abc-123")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Service).NotTo(BeNil())
			Expect(result.Service.Name).NotTo(BeNil())
			Expect(*result.Service.Name).To(Equal("my-app"))
			Expect(result.Service.ExternalIp).NotTo(BeNil())
			Expect(*result.Service.ExternalIp).To(Equal("203.0.113.1"))
		})

		// TC-I075: Get populates update_time from Pod condition transition
		It("populates update_time from Pod condition transition (TC-I075)", func() {
			s, client := newTestStore(defaultConfig())

			err := createFakeDeployment(client, "my-app", "abc-123")
			Expect(err).NotTo(HaveOccurred())

			transitionTime := time.Date(2026, 2, 18, 10, 0, 0, 0, time.UTC)
			err = createFakePod(client, "my-app-pod", "abc-123",
				corev1.PodRunning, "10.0.0.1",
				withPodConditions([]corev1.PodCondition{
					{
						Type:               corev1.PodReady,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(transitionTime),
					},
				}),
			)
			Expect(err).NotTo(HaveOccurred())

			result, err := s.Get(context.Background(), "abc-123")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.UpdateTime).NotTo(BeNil())
			Expect(result.UpdateTime.UTC()).To(Equal(transitionTime))
		})

		// TC-I076: Get populates update_time from Deployment condition when no Pod
		It("populates update_time from Deployment condition when no Pod (TC-I076)", func() {
			s, client := newTestStore(defaultConfig())

			transitionTime := time.Date(2026, 2, 18, 9, 0, 0, 0, time.UTC)
			err := createFakeDeployment(client, "my-app", "abc-123",
				withDeploymentConditions([]appsv1.DeploymentCondition{
					{
						Type:               appsv1.DeploymentAvailable,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(transitionTime),
					},
				}),
			)
			Expect(err).NotTo(HaveOccurred())

			result, err := s.Get(context.Background(), "abc-123")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.UpdateTime).NotTo(BeNil())
			Expect(result.UpdateTime.UTC()).To(Equal(transitionTime))
		})

		// TC-I102: Get returns conflict when multiple Deployments share the same instance ID
		It("returns conflict when multiple Deployments share the same instance ID (TC-I102)", func() {
			s, client := newTestStore(defaultConfig())

			// Create two Deployments with the same instance ID but different names
			err := createFakeDeployment(client, "app-one", "dup-id")
			Expect(err).NotTo(HaveOccurred())
			err = createFakeDeployment(client, "app-two", "dup-id")
			Expect(err).NotTo(HaveOccurred())

			_, err = s.Get(context.Background(), "dup-id")

			var conflictErr *store.ConflictError
			Expect(errors.As(err, &conflictErr)).To(BeTrue(), "expected ConflictError, got: %v", err)
		})

		// TC-I077: Get returns container without service data when no Service
		It("returns container without service data when no Service (TC-I077)", func() {
			s, client := newTestStore(defaultConfig())

			err := createFakeDeployment(client, "my-app", "abc-123")
			Expect(err).NotTo(HaveOccurred())
			err = createFakePod(client, "my-app-pod", "abc-123", corev1.PodRunning, "10.0.0.1")
			Expect(err).NotTo(HaveOccurred())

			result, err := s.Get(context.Background(), "abc-123")
			Expect(err).NotTo(HaveOccurred())

			// Status and network IP should be present
			Expect(result.Status).NotTo(BeNil())
			Expect(result.Spec.Network).NotTo(BeNil())
			Expect(result.Spec.Network.Ip).NotTo(BeNil())

			// Service should be nil when no Service exists
			Expect(result.Service).To(BeNil())
		})
	})
})
