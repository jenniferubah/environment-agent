package registration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	dcmv1alpha1 "github.com/dcm-project/service-provider-manager/api/v1alpha1/provider"

	"github.com/dcm-project/environment-agent/internal/openshift/container/config"
	"github.com/dcm-project/environment-agent/internal/openshift/container/registration"
)

// syncBuffer wraps bytes.Buffer with a mutex to make it safe for concurrent use
// as an slog output target. The registration goroutine writes via slog while
// test goroutines read (String) and reset (Reset) concurrently.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

var _ = Describe("Registration Integration", func() {
	var (
		mockServer *httptest.Server
		cfg        *config.Config
		logBuf     *syncBuffer
		logger     *slog.Logger
	)

	BeforeEach(func() {
		logBuf = &syncBuffer{}
		logger = slog.New(slog.NewJSONHandler(logBuf, nil))
	})

	AfterEach(func() {
		if mockServer != nil {
			mockServer.Close()
		}
	})

	// TC-I053: SP registers with DCM on startup
	It("sends POST to {registrationUrl}/providers on startup (TC-I053)", func() {
		var requestReceived atomic.Bool

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				requestReceived.Store(true)
			}
			w.WriteHeader(http.StatusOK)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		Eventually(requestReceived.Load).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(BeTrue(),
			"expected POST to /providers but no request was received")
	})

	// TC-I054: Registration payload contains correct fields
	It("sends payload with all expected fields including metadata (TC-I054)", func() {
		var receivedPayload dcmv1alpha1.Provider
		var requestReceived atomic.Bool

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				defer func() { _ = r.Body.Close() }()
				body, err := io.ReadAll(r.Body)
				if err == nil {
					_ = json.Unmarshal(body, &receivedPayload)
					requestReceived.Store(true)
				}
			}
			w.WriteHeader(http.StatusOK)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s SP",
				Endpoint:    "https://sp.example.com",
				Region:      "us-east-1",
				Zone:        "us-east-1a",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		Eventually(requestReceived.Load).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(BeTrue(),
			"expected registration request but none was received")

		Expect(receivedPayload.Name).To(Equal("k8s-sp"))
		Expect(receivedPayload.ServiceType).To(Equal("container"))
		Expect(receivedPayload.DisplayName).To(HaveValue(Equal("K8s SP")))
		Expect(receivedPayload.Endpoint).To(Equal("https://sp.example.com/api/v1alpha1/containers"))
		Expect(receivedPayload.Operations).To(HaveValue(ConsistOf("CREATE", "DELETE", "READ")))
		Expect(receivedPayload.SchemaVersion).To(Equal("v1alpha1"))
		Expect(receivedPayload.Metadata).NotTo(BeNil())
		Expect(receivedPayload.Metadata.RegionCode).To(HaveValue(Equal("us-east-1")))
		Expect(receivedPayload.Metadata.Zone).To(HaveValue(Equal("us-east-1a")))
	})

	// TC-I055: Registration does not block server startup
	It("Start() returns within 1s; registration completes in background (TC-I055)", func() {
		var requestReceived atomic.Bool

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate a slow DCM registry (5s per test plan)
			time.Sleep(5 * time.Second)
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				requestReceived.Store(true)
			}
			w.WriteHeader(http.StatusOK)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start() must return quickly (non-blocking)
		startTime := time.Now()
		registrar.Start(ctx)
		elapsed := time.Since(startTime)
		Expect(elapsed).To(BeNumerically("<", 1*time.Second),
			"Start() must return in under 1 second")

		// Registration should complete in the background
		Eventually(requestReceived.Load).WithTimeout(10*time.Second).WithPolling(200*time.Millisecond).Should(BeTrue(),
			"expected registration to complete in background")
	})

	// TC-I056: Registration retries with exponential backoff
	It("retries with increasing intervals and succeeds on 4th attempt (TC-I056)", func() {
		var requestCount atomic.Int32
		var requestTimes []time.Time
		var mu sync.Mutex

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				count := requestCount.Add(1)
				mu.Lock()
				requestTimes = append(requestTimes, time.Now())
				mu.Unlock()

				if count < 4 {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
			}
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger,
			registration.SetInitialBackoff(10*time.Millisecond),
			registration.SetMaxBackoff(200*time.Millisecond),
		)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		Eventually(requestCount.Load).WithTimeout(5*time.Second).WithPolling(50*time.Millisecond).Should(BeNumerically(">=", int32(4)),
			"expected at least 4 registration attempts")

		// Verify increasing intervals between requests
		mu.Lock()
		defer mu.Unlock()
		Expect(requestTimes).To(HaveLen(4))
		for i := 2; i < len(requestTimes); i++ {
			prev := requestTimes[i-1].Sub(requestTimes[i-2])
			curr := requestTimes[i].Sub(requestTimes[i-1])
			Expect(curr).To(BeNumerically(">=", prev),
				"interval between attempts should increase (attempt %d)", i+1)
		}
	})

	// TC-I057: Registration failure is logged without causing SP exit
	It("logs errors and keeps registrar running on failure (TC-I057)", func() {
		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger,
			registration.SetInitialBackoff(10*time.Millisecond),
			registration.SetMaxBackoff(50*time.Millisecond),
		)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		Eventually(func() string {
			return logBuf.String()
		}).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(
			And(
				ContainSubstring("registration"),
				ContainSubstring("\"level\":\"WARN\""),
			),
			"expected WARN-level log entries about registration failures")
	})

	// TC-I058: Re-registration sends idempotent payload
	It("sends identical payload on re-registration (TC-I058)", func() {
		var payloads []dcmv1alpha1.Provider
		var mu sync.Mutex
		var requestCount atomic.Int32

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				defer func() { _ = r.Body.Close() }()
				body, err := io.ReadAll(r.Body)
				if err == nil {
					var p dcmv1alpha1.Provider
					if json.Unmarshal(body, &p) == nil {
						mu.Lock()
						payloads = append(payloads, p)
						mu.Unlock()
						requestCount.Add(1)
					}
				}
			}
			w.WriteHeader(http.StatusOK)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
				Region:      "us-east-1",
				Zone:        "us-east-1a",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		// First registration
		registrar1, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx1, cancel1 := context.WithCancel(context.Background())
		registrar1.Start(ctx1)

		Eventually(requestCount.Load).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(BeNumerically(">=", int32(1)),
			"expected first registration request")
		cancel1()

		// Second registration (simulates restart)
		registrar2, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx2, cancel2 := context.WithCancel(context.Background())
		defer cancel2()
		registrar2.Start(ctx2)

		Eventually(requestCount.Load).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(BeNumerically(">=", int32(2)),
			"expected second registration request")

		mu.Lock()
		defer mu.Unlock()
		Expect(payloads).To(HaveLen(2))
		Expect(payloads[0]).To(Equal(payloads[1]),
			"payloads from two registrations should be identical")
	})

	// TC-I059: Registration uses DCM client library request format
	It("request format matches DCM client library output (TC-I059)", func() {
		var receivedBody []byte
		var requestReceived atomic.Bool

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				defer func() { _ = r.Body.Close() }()
				var err error
				receivedBody, err = io.ReadAll(r.Body)
				if err == nil {
					requestReceived.Store(true)
				}
			}
			w.WriteHeader(http.StatusOK)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		Eventually(requestReceived.Load).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(BeTrue(),
			"expected registration request but none was received")

		// Verify the body is valid JSON with expected structure
		var parsed map[string]any
		Expect(json.Unmarshal(receivedBody, &parsed)).To(Succeed(),
			"request body should be valid JSON")
		Expect(parsed).To(HaveKey("name"))
		Expect(parsed).To(HaveKey("service_type"))
		Expect(parsed).To(HaveKey("display_name"))
		Expect(parsed).To(HaveKey("endpoint"))
		Expect(parsed).To(HaveKey("operations"))
		Expect(parsed).To(HaveKey("schema_version"))
	})

	// TC-I068: Registration omits optional fields when not configured
	It("omits optional fields when not configured (TC-I068)", func() {
		var receivedBody []byte
		var requestReceived atomic.Bool

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				defer func() { _ = r.Body.Close() }()
				var err error
				receivedBody, err = io.ReadAll(r.Body)
				if err == nil {
					requestReceived.Store(true)
				}
			}
			w.WriteHeader(http.StatusOK)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:     "k8s-sp",
				Endpoint: "https://sp.example.com",
				// No DisplayName, Region, or Zone set
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		Eventually(requestReceived.Load).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(BeTrue(),
			"expected registration request but none was received")

		// Verify optional fields are absent
		var parsed map[string]any
		Expect(json.Unmarshal(receivedBody, &parsed)).To(Succeed())
		Expect(parsed).To(HaveKey("name"))
		Expect(parsed).NotTo(HaveKey("display_name"),
			"display_name should be absent when not configured")
		Expect(parsed).NotTo(HaveKey("metadata"),
			"metadata should be absent when region and zone are not configured")
	})

	// TC-I083: Multiple Start() calls launch only one goroutine
	It("multiple Start() calls launch only one goroutine (TC-I083)", func() {
		var requestCount atomic.Int32

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				requestCount.Add(1)
			}
			w.WriteHeader(http.StatusOK)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Call Start() three times.
		registrar.Start(ctx)
		registrar.Start(ctx)
		registrar.Start(ctx)

		// Wait for the registration attempt to complete.
		Eventually(requestCount.Load).WithTimeout(3*time.Second).WithPolling(50*time.Millisecond).Should(BeNumerically(">=", int32(1)),
			"expected at least one registration attempt")

		// Give extra time to ensure no additional goroutines sent requests.
		time.Sleep(200 * time.Millisecond)

		Expect(requestCount.Load()).To(Equal(int32(1)),
			"expected exactly 1 registration attempt from 3 Start() calls")
	})

	// TC-I084: Done() channel closes after context cancellation
	It("Done() channel closes after context cancellation (TC-I084)", func() {
		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger,
			registration.SetInitialBackoff(10*time.Millisecond),
			registration.SetMaxBackoff(50*time.Millisecond),
		)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())

		registrar.Start(ctx)

		// Let it retry a few times.
		time.Sleep(100 * time.Millisecond)

		// Cancel the context.
		cancel()

		// Done() channel should close.
		Eventually(registrar.Done()).WithTimeout(3*time.Second).Should(BeClosed(),
			"Done() channel should close after context cancellation")
	})

	// TC-I104: Registration stops retrying on 4xx client error
	It("stops retrying on 4xx client error (TC-I104)", func() {
		var requestCount atomic.Int32

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				requestCount.Add(1)
			}
			w.WriteHeader(http.StatusBadRequest)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger,
			registration.SetInitialBackoff(10*time.Millisecond),
			registration.SetMaxBackoff(50*time.Millisecond),
		)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		// Done() channel should close because 4xx is non-retryable.
		Eventually(registrar.Done()).WithTimeout(3*time.Second).Should(BeClosed(),
			"Done() channel should close after non-retryable 4xx error")

		// Only one request should have been made (no retries).
		Expect(requestCount.Load()).To(Equal(int32(1)),
			"expected exactly 1 registration attempt, no retries for 4xx")

		// Error should be logged at ERROR level.
		Expect(logBuf.String()).To(ContainSubstring(`"level":"ERROR"`))
		Expect(logBuf.String()).To(ContainSubstring("non-retryable"))
	})

	// TC-I087: Done() channel closes after successful registration
	It("Done() channel closes after successful registration (TC-I087)", func() {
		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		// Done() channel should close after the successful 200 response,
		// without requiring context cancellation.
		Eventually(registrar.Done()).WithTimeout(3*time.Second).Should(BeClosed(),
			"Done() channel should close after successful registration")
	})

	// TC-I080: Backoff interval never exceeds configured max cap
	It("backoff interval never exceeds configured maximum cap (TC-I080)", func() {
		var requestTimes []time.Time
		var mu sync.Mutex
		var requestCount atomic.Int32

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				mu.Lock()
				requestTimes = append(requestTimes, time.Now())
				mu.Unlock()
				requestCount.Add(1)
			}
			w.WriteHeader(http.StatusInternalServerError)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-sp",
				DisplayName: "K8s Container SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		maxCap := 200 * time.Millisecond
		registrar, err := registration.NewRegistrar(cfg, logger,
			registration.SetInitialBackoff(10*time.Millisecond),
			registration.SetMaxBackoff(maxCap),
		)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		// Wait for enough attempts to exceed the cap if uncapped
		Eventually(requestCount.Load).WithTimeout(8*time.Second).WithPolling(50*time.Millisecond).Should(BeNumerically(">=", int32(8)),
			"expected at least 8 registration attempts")

		// Verify no interval exceeds the max cap (with tolerance for scheduling jitter)
		mu.Lock()
		defer mu.Unlock()
		tolerance := 50 * time.Millisecond
		for i := 1; i < len(requestTimes); i++ {
			interval := requestTimes[i].Sub(requestTimes[i-1])
			Expect(interval).To(BeNumerically("<=", maxCap+tolerance),
				"interval between attempt %d and %d was %v, exceeding max cap %v",
				i, i+1, interval, maxCap)
		}
	})
})
