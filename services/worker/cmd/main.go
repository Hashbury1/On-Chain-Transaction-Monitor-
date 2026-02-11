package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"net/http"
)

var (
	logger = logrus.New()

	// Prometheus metrics
	notificationsSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notifications_sent_total",
			Help: "Total notifications sent",
		},
		[]string{"destination", "status"},
	)

	notificationLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "notification_latency_seconds",
			Help:    "Latency from event to notification delivery",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
		},
		[]string{"destination"},
	)

	retryAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "retry_attempts_total",
			Help: "Total retry attempts",
		},
		[]string{"retry_number"},
	)

	dlqMessages = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dlq_messages_total",
			Help: "Total messages sent to dead letter queue",
		},
	)

	workerProcessingDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "worker_processing_duration_seconds",
			Help:    "Time spent processing messages",
			Buckets: prometheus.DefBuckets,
		},
	)

	circuitBreakerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "circuit_breaker_state",
			Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
		},
		[]string{"destination"},
	)
)

func init() {
	prometheus.MustRegister(notificationsSent)
	prometheus.MustRegister(notificationLatency)
	prometheus.MustRegister(retryAttempts)
	prometheus.MustRegister(dlqMessages)
	prometheus.MustRegister(workerProcessingDuration)
	prometheus.MustRegister(circuitBreakerState)

	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetOutput(os.Stdout)
	
	level, _ := logrus.ParseLevel(getEnv("LOG_LEVEL", "info"))
	logger.SetLevel(level)
}

type Config struct {
	QueueURL               string
	QueueType              string
	MetricsPort            string
	MaxRetryAttempts       int
	RetryBackoffMultiplier int
	RetryMaxBackoffSeconds int
	WorkerCount            int
}

func LoadConfig() *Config {
	return &Config{
		QueueURL:               getEnv("RABBITMQ_URL", ""),
		QueueType:              getEnv("MESSAGE_QUEUE_TYPE", "rabbitmq"),
		MetricsPort:            getEnv("METRICS_PORT_WORKER", "8082"),
		MaxRetryAttempts:       getEnvAsInt("MAX_RETRY_ATTEMPTS", 5),
		RetryBackoffMultiplier: getEnvAsInt("RETRY_BACKOFF_MULTIPLIER", 2),
		RetryMaxBackoffSeconds: getEnvAsInt("RETRY_MAX_BACKOFF_SECONDS", 60),
		WorkerCount:            getEnvAsInt("WORKER_COUNT", 5),
	}
}

func main() {
	logger.Info("Starting Notification Worker Service")

	config := LoadConfig()

	// Start metrics server
	go startMetricsServer(config.MetricsPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start worker pool (simulation mode)
	for i := 0; i < config.WorkerCount; i++ {
		workerID := i
		go func() {
			simulateWorker(ctx, workerID, config)
		}()
	}

	logger.Infof("Started %d workers (simulation mode)", config.WorkerCount)

	// Wait for shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	
	sig := <-sigChan
	logger.Infof("Received signal: %v", sig)

	logger.Info("Shutting down gracefully...")
	cancel()
	time.Sleep(2 * time.Second)
	logger.Info("Shutdown complete")
}

func simulateWorker(ctx context.Context, id int, config *Config) {
	logger.Infof("Worker %d starting", id)
	
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Infof("Worker %d stopping", id)
			return
		case <-ticker.C:
			// Simulate processing a notification
			startTime := time.Now()
			
			// Randomly pick a destination
			destinations := []string{"discord", "slack", "webhook"}
			dest := destinations[rand.Intn(len(destinations))]
			
			// Simulate sending with some random success/failure
			success := rand.Float64() > 0.05 // 95% success rate
			
			if success {
				notificationsSent.WithLabelValues(dest, "success").Inc()
				latency := 0.1 + rand.Float64()*2 // 0.1-2.1 seconds
				notificationLatency.WithLabelValues(dest).Observe(latency)
				
				logger.WithFields(logrus.Fields{
					"worker":      id,
					"destination": dest,
					"latency":     fmt.Sprintf("%.2fs", latency),
				}).Info("Notification sent (simulated)")
			} else {
				// Simulate failure and retry
				attempts := rand.Intn(config.MaxRetryAttempts) + 1
				for attempt := 0; attempt < attempts; attempt++ {
					retryAttempts.WithLabelValues(fmt.Sprintf("%d", attempt)).Inc()
					time.Sleep(calculateBackoff(attempt, config))
				}
				
				if attempts >= config.MaxRetryAttempts {
					notificationsSent.WithLabelValues(dest, "failed").Inc()
					dlqMessages.Inc()
					logger.WithFields(logrus.Fields{
						"worker":      id,
						"destination": dest,
						"attempts":    attempts,
					}).Warn("Notification failed after retries (simulated)")
				} else {
					notificationsSent.WithLabelValues(dest, "success").Inc()
					logger.WithFields(logrus.Fields{
						"worker":      id,
						"destination": dest,
						"attempts":    attempts,
					}).Info("Notification sent after retry (simulated)")
				}
			}
			
			workerProcessingDuration.Observe(time.Since(startTime).Seconds())
		}
	}
}

func calculateBackoff(attempt int, config *Config) time.Duration {
	backoffSeconds := math.Pow(float64(config.RetryBackoffMultiplier), float64(attempt))
	
	if backoffSeconds > float64(config.RetryMaxBackoffSeconds) {
		backoffSeconds = float64(config.RetryMaxBackoffSeconds)
	}

	// Add jitter (±20%)
	jitter := backoffSeconds * 0.2 * (rand.Float64()*2 - 1)
	backoff := time.Duration(backoffSeconds+jitter) * time.Second

	return backoff
}

func startMetricsServer(port string) {
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("READY"))
	})

	addr := fmt.Sprintf(":%s", port)
	logger.Infof("Starting metrics server on %s", addr)
	logger.Infof("Metrics: http://localhost:%s/metrics", port)
	logger.Infof("Health: http://localhost:%s/health", port)
	
	if err := http.ListenAndServe(addr, nil); err != nil {
		logger.Fatalf("Metrics server failed: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var result int
		fmt.Sscanf(value, "%d", &result)
		return result
	}
	return defaultValue
}