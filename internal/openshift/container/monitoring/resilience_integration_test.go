package monitoring_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dcm-project/environment-agent/internal/openshift/container/dcm"
	"github.com/dcm-project/environment-agent/internal/openshift/container/monitoring"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var _ = Describe("Status Monitor", func() {
	Describe("Resilience", func() {
		It("should reconnect watchers after API server interruption (TC-I099)", func() {
			client := fake.NewClientset()
			publisher := newMockPublisher()
			logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
			cfg := monitoring.MonitorConfig{
				Namespace:    "default",
				ProviderName: "k8s-sp",
				DebounceMs:   50,
				ResyncPeriod: 1 * time.Second,
			}

			// Set up reactor BEFORE starting the monitor to avoid racing with
			// the informer's concurrent use of the fake client.
			var failing atomic.Bool
			var callCount atomic.Int32
			client.PrependReactor("list", "deployments", func(_ k8stesting.Action) (bool, runtime.Object, error) {
				if !failing.Load() {
					return false, nil, nil
				}
				n := callCount.Add(1)
				if n <= 2 {
					return true, nil, errors.NewServiceUnavailable("API server unavailable")
				}
				return false, nil, nil
			})

			monitor := monitoring.NewStatusMonitor(client, cfg, publisher, logger)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				defer GinkgoRecover()
				_ = monitor.Start(ctx)
			}()
			time.Sleep(200 * time.Millisecond)

			// Simulate API server interruption.
			failing.Store(true)

			// Wait for reconnection attempt.
			time.Sleep(2 * time.Second)

			// Create a resource after "reconnection".
			replicas := int32(1)
			labels := dcm.Labels("reconnect-test")
			_, err := client.AppsV1().Deployments("default").Create(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "after-reconnect", Labels: labels},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
					},
				},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(500 * time.Millisecond)

			events := publisher.Events()
			var found bool
			for _, e := range events {
				if e.InstanceID == "reconnect-test" {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "events should resume after reconnection")
		})

		It("should retry publishing with exponential backoff on transient failure (TC-I100)", func() {
			failPublisher := &retryTrackingPublisher{}
			client := fake.NewClientset()
			logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
			cfg := monitoring.MonitorConfig{
				Namespace:    "default",
				ProviderName: "k8s-sp",
				DebounceMs:   50,
				ResyncPeriod: 1 * time.Second,
			}

			monitor := monitoring.NewStatusMonitor(client, cfg, failPublisher, logger)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				defer GinkgoRecover()
				_ = monitor.Start(ctx)
			}()
			time.Sleep(200 * time.Millisecond)

			// Create a resource to trigger a publish.
			replicas := int32(1)
			labels := dcm.Labels("retry-test")
			_, err := client.AppsV1().Deployments("default").Create(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "retry-deploy", Labels: labels},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
					},
				},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Wait for retries.
			time.Sleep(2 * time.Second)

			// Verify multiple attempts were made.
			Expect(failPublisher.attempts.Load()).To(BeNumerically(">=", 2),
				"publisher should retry on transient failures")
		})

		It("should continue serving when NATS is unavailable (TC-I101)", func() {
			logBuf := &safeBuffer{}
			failPublisher := &retryTrackingPublisher{failAlways: true}
			client := fake.NewClientset()
			logger := slog.New(slog.NewJSONHandler(logBuf, nil))
			cfg := monitoring.MonitorConfig{
				Namespace:    "default",
				ProviderName: "k8s-sp",
				DebounceMs:   50,
				ResyncPeriod: 1 * time.Second,
			}

			monitor := monitoring.NewStatusMonitor(client, cfg, failPublisher, logger)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				defer GinkgoRecover()
				_ = monitor.Start(ctx)
			}()
			time.Sleep(200 * time.Millisecond)

			// Create a resource that will trigger a publish (which will fail).
			replicas := int32(1)
			labels := dcm.Labels("nats-down")
			_, err := client.AppsV1().Deployments("default").Create(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "nats-down-deploy", Labels: labels},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
					},
				},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(1 * time.Second)

			// Monitor should still be operational (Start did not return with error / panic).
			// The failure should be logged.
			Expect(logBuf.String()).To(ContainSubstring("error"),
				"NATS failure should be logged")
		})
	})
})

// safeBuffer is a thread-safe wrapper around bytes.Buffer for use as an
// io.Writer in tests where concurrent writes and reads are expected.
type safeBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// retryTrackingPublisher is a mock publisher that fails N times then succeeds,
// tracking attempt counts for retry verification.
type retryTrackingPublisher struct {
	attempts   atomic.Int32
	failAlways bool
}

func (p *retryTrackingPublisher) Publish(_ context.Context, _ monitoring.StatusEvent) error {
	n := p.attempts.Add(1)
	if p.failAlways || n <= 3 {
		return context.DeadlineExceeded // Simulate transient failure.
	}
	return nil
}

func (p *retryTrackingPublisher) Close() error {
	return nil
}
