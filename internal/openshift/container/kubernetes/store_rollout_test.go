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
)

var _ = Describe("K8s Store", func() {
	Describe("Rolling Update Pod Handling", func() {
		// TC-I107: Get returns Running pod when 2 pods exist during rollout
		It("returns Running pod when 2 pods exist during rollout (TC-I107)", func() {
			s, client := newTestStore(defaultConfig())

			// Create Deployment mid-rollout: UpdatedReplicas < Replicas
			err := createFakeDeployment(client, "my-app", "abc-123",
				withDeploymentStatus(appsv1.DeploymentStatus{
					Replicas:            2,
					UpdatedReplicas:     1,
					UnavailableReplicas: 1,
				}))
			Expect(err).NotTo(HaveOccurred())

			// Running pod (old) and Pending pod (new)
			err = createFakePod(client, "my-app-old", "abc-123", corev1.PodRunning, "10.0.0.1")
			Expect(err).NotTo(HaveOccurred())
			err = createFakePod(client, "my-app-new", "abc-123", corev1.PodPending, "")
			Expect(err).NotTo(HaveOccurred())

			result, err := s.Get(context.Background(), "abc-123")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).NotTo(BeNil())
			Expect(*result.Status).To(Equal(v1alpha1.RUNNING))
			Expect(result.Spec.Network).NotTo(BeNil())
			Expect(result.Spec.Network.Ip).NotTo(BeNil())
			Expect(*result.Spec.Network.Ip).To(Equal("10.0.0.1"))
		})

		// TC-I108: Get returns newest pod when 2 pods exist during rollout (both Pending)
		It("returns newest pod when 2 pods exist during rollout and none Running (TC-I108)", func() {
			s, client := newTestStore(defaultConfig())

			// Create Deployment mid-rollout
			err := createFakeDeployment(client, "my-app", "abc-123",
				withDeploymentStatus(appsv1.DeploymentStatus{
					Replicas:            2,
					UpdatedReplicas:     1,
					UnavailableReplicas: 1,
				}))
			Expect(err).NotTo(HaveOccurred())

			// Both pods Pending, older one created first
			oldTime := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
			newTime := time.Date(2026, 3, 4, 10, 1, 0, 0, time.UTC)

			err = createFakePod(client, "my-app-old", "abc-123",
				corev1.PodPending, "", withCreationTime(oldTime))
			Expect(err).NotTo(HaveOccurred())
			err = createFakePod(client, "my-app-new", "abc-123",
				corev1.PodPending, "", withCreationTime(newTime))
			Expect(err).NotTo(HaveOccurred())

			result, err := s.Get(context.Background(), "abc-123")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status).NotTo(BeNil())
			Expect(*result.Status).To(Equal(v1alpha1.PENDING))
		})

		// TC-I109: Get returns ConflictError when 2 pods exist but NO rollout in progress
		It("returns ConflictError when 2 pods exist but no rollout (TC-I109)", func() {
			s, client := newTestStore(defaultConfig())

			// Deployment with stable status (no rollout)
			err := createFakeDeployment(client, "my-app", "abc-123",
				withDeploymentStatus(appsv1.DeploymentStatus{
					Replicas:            1,
					UpdatedReplicas:     1,
					UnavailableReplicas: 0,
				}))
			Expect(err).NotTo(HaveOccurred())

			// Two pods — anomalous without a rollout
			err = createFakePod(client, "my-app-1", "abc-123", corev1.PodRunning, "10.0.0.1")
			Expect(err).NotTo(HaveOccurred())
			err = createFakePod(client, "my-app-2", "abc-123", corev1.PodRunning, "10.0.0.2")
			Expect(err).NotTo(HaveOccurred())

			_, err = s.Get(context.Background(), "abc-123")

			var conflictErr *store.ConflictError
			Expect(errors.As(err, &conflictErr)).To(BeTrue(), "expected ConflictError, got: %v", err)
		})

		// TC-I110: Get returns ConflictError when 3+ pods exist regardless of rollout
		It("returns ConflictError when 3+ pods exist regardless of rollout (TC-I110)", func() {
			s, client := newTestStore(defaultConfig())

			// Deployment mid-rollout
			err := createFakeDeployment(client, "my-app", "abc-123",
				withDeploymentStatus(appsv1.DeploymentStatus{
					Replicas:            2,
					UpdatedReplicas:     1,
					UnavailableReplicas: 1,
				}))
			Expect(err).NotTo(HaveOccurred())

			// 3 pods — too many even during rollout
			err = createFakePod(client, "my-app-1", "abc-123", corev1.PodRunning, "10.0.0.1")
			Expect(err).NotTo(HaveOccurred())
			err = createFakePod(client, "my-app-2", "abc-123", corev1.PodPending, "")
			Expect(err).NotTo(HaveOccurred())
			err = createFakePod(client, "my-app-3", "abc-123", corev1.PodPending, "")
			Expect(err).NotTo(HaveOccurred())

			_, err = s.Get(context.Background(), "abc-123")

			var conflictErr *store.ConflictError
			Expect(errors.As(err, &conflictErr)).To(BeTrue(), "expected ConflictError, got: %v", err)
		})
	})
})
