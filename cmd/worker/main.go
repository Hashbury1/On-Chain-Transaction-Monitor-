package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
	logger = logrus.New()

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
			Help:    "Notification delivery latency",
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
			Help: "Messages sent to dead letter queue",
		},
	)
)

func init() {
	prometheus.MustRegister(notificationsSent)
	prometheus.MustRegister(notificationLatency)
	prometheus.MustRegister(retryAttempts)
	prometheus.MustRegister(dlqMessages)

	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetOutput(os.Stdout)
	
	level, _ := logrus.ParseLevel(getEnv("LOG_LEVEL", "info"))
	logger.SetLevel(level)
}

type Config struct {
	MetricsPort            string
	MaxRetryAttempts       int
	RetryBackoffMultiplier int
	RetryMaxBackoffSeconds int
	WorkerCount            int
}

func LoadConfig() *Config {
	return &Config{
		MetricsPort:            getEnv("METRICS_PORT_WORKER", "8082"),
		MaxRetryAttempts:       getEnvAsInt("MAX_RETRY_ATTEMPTS", 5),
		RetryBackoffMultiplier: getEnvAsInt("RETRY_BACKOFF_MULTIPLIER", 2),
		RetryMaxBackoffSeconds: getEnvAsInt("RETRY_MAX_BACKOFF_SECONDS", 60),
		WorkerCount:            getEnvAsInt("WORKER_COUNT", 5),
	}
}

func main() {
	logger.Info("Starting On-Chain Transaction Monitor - Worker Service")

	config := LoadConfig()
	go startMetricsServer(config.MetricsPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < config.WorkerCount; i++ {
		workerID := i
		go simulateWorker(ctx, workerID, config)
	}

	logger.Infof("Started %d workers (simulation mode)", config.WorkerCount)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

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
			destinations := []string{"discord", "slack", "webhook"}
			dest := destinations[rand.Intn(len(destinations))]
			
			success := rand.Float64() > 0.05
			
			if success {
				notificationsSent.WithLabelValues(dest, "success").Inc()
				latency := 0.1 + rand.Float64()*2
				notificationLatency.WithLabelValues(dest).Observe(latency)
				
				logger.WithFields(logrus.Fields{
					"worker":      id,
					"destination": dest,
					"latency":     fmt.Sprintf("%.2fs", latency),
				}).Info("Notification sent (simulated)")
			} else {
				attempts := rand.Intn(config.MaxRetryAttempts) + 1
				for attempt := 0; attempt < attempts; attempt++ {
					retryAttempts.WithLabelValues(fmt.Sprintf("%d", attempt)).Inc()
					time.Sleep(calculateBackoff(attempt, config))
				}
				
				if attempts >= config.MaxRetryAttempts {
					notificationsSent.WithLabelValues(dest, "failed").Inc()
					dlqMessages.Inc()
					logger.WithFields(logrus.Fields{
						"worker": id,
						"destination": dest,
					}).Warn("Notification failed after retries (simulated)")
				}
			}
		}
	}
}

func calculateBackoff(attempt int, config *Config) time.Duration {
	backoff := math.Pow(float64(config.RetryBackoffMultiplier), float64(attempt))
	if backoff > float64(config.RetryMaxBackoffSeconds) {
		backoff = float64(config.RetryMaxBackoffSeconds)
	}
	jitter := backoff * 0.2 * (rand.Float64()*2 - 1)
	return time.Duration(backoff+jitter) * time.Second
}

func startMetricsServer(port string) {
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf(":%s", port)
	logger.Infof("Metrics server: http://localhost:%s/metrics", port)
	
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
