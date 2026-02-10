package main

import (
	"context"
	"encoding/json"
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
	log = logrus.New()

	// Prometheus metrics
	notificationsSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notifications_sent_total",
			Help: "Total notifications sent",
		},
		[]string{"destination", "status"}, // status: success, failed, retrying
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

	log.SetFormatter(&logrus.JSONFormatter{})
	log.SetOutput(os.Stdout)
	
	level, _ := logrus.ParseLevel(getEnv("LOG_LEVEL", "info"))
	log.SetLevel(level)
}

type Config struct {
	QueueURL               string
	QueueType              string
	DatabaseURL            string
	RedisURL               string
	MetricsPort            string
	MaxRetryAttempts       int
	RetryBackoffMultiplier int
	RetryMaxBackoffSeconds int
	CircuitBreakerThreshold int
	CircuitBreakerTimeout  time.Duration
	WorkerCount            int
}

func LoadConfig() *Config {
	return &Config{
		QueueURL:               getEnv("RABBITMQ_URL", ""),
		QueueType:              getEnv("MESSAGE_QUEUE_TYPE", "rabbitmq"),
		DatabaseURL:            getEnv("DATABASE_URL", ""),
		RedisURL:               getEnv("REDIS_URL", ""),
		MetricsPort:            getEnv("METRICS_PORT", "8082"),
		MaxRetryAttempts:       getEnvAsInt("MAX_RETRY_ATTEMPTS", 5),
		RetryBackoffMultiplier: getEnvAsInt("RETRY_BACKOFF_MULTIPLIER", 2),
		RetryMaxBackoffSeconds: getEnvAsInt("RETRY_MAX_BACKOFF_SECONDS", 60),
		CircuitBreakerThreshold: getEnvAsInt("CIRCUIT_BREAKER_THRESHOLD", 5),
		CircuitBreakerTimeout:  time.Duration(getEnvAsInt("CIRCUIT_BREAKER_TIMEOUT_SECONDS", 30)) * time.Second,
		WorkerCount:            getEnvAsInt("WORKER_COUNT", 5),
	}
}

func main() {
	log.Info("Starting Notification Worker Service")

	config := LoadConfig()

	// Start metrics server
	go startMetricsServer(config.MetricsPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize components
	db, err := initDatabase(config.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	cache, err := initRedis(config.RedisURL)
	if err != nil {
		log.Fatalf("Failed to initialize Redis: %v", err)
	}
	defer cache.Close()

	queue, err := initQueue(config.QueueType, config.QueueURL)
	if err != nil {
		log.Fatalf("Failed to initialize message queue: %v", err)
	}
	defer queue.Close()

	// Initialize circuit breakers for each destination
	cbManager := NewCircuitBreakerManager(config)

	// Initialize notifiers
	notifierPool := NewNotifierPool(config, db, cache, cbManager)

	// Start worker pool
	for i := 0; i < config.WorkerCount; i++ {
		workerID := i
		go func() {
			worker := NewWorker(workerID, config, queue, notifierPool, db)
			if err := worker.Start(ctx); err != nil {
				log.Errorf("Worker %d error: %v", workerID, err)
			}
		}()
	}

	log.Infof("Started %d workers", config.WorkerCount)

	// Wait for shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Info("Shutting down gracefully...")
	cancel()
	time.Sleep(5 * time.Second)
	log.Info("Shutdown complete")
}

// Worker processes messages from the queue
type Worker struct {
	id       int
	config   *Config
	queue    MessageQueue
	notifier *NotifierPool
	db       Database
}

func NewWorker(id int, config *Config, queue MessageQueue, notifier *NotifierPool, db Database) *Worker {
	return &Worker{
		id:       id,
		config:   config,
		queue:    queue,
		notifier: notifier,
		db:       db,
	}
}

func (w *Worker) Start(ctx context.Context) error {
	log.Infof("Worker %d starting", w.id)

	// Subscribe to queue
	messages, err := w.queue.Subscribe(ctx, "blockchain-events")
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Infof("Worker %d stopping", w.id)
			return nil
		case msg := <-messages:
			startTime := time.Now()
			w.processMessage(ctx, msg)
			workerProcessingDuration.Observe(time.Since(startTime).Seconds())
		}
	}
}

func (w *Worker) processMessage(ctx context.Context, msg *QueueMessage) {
	var notification NotificationMessage
	if err := json.Unmarshal(msg.Body, &notification); err != nil {
		log.Errorf("Failed to unmarshal message: %v", err)
		msg.Ack() // Invalid message, don't retry
		return
	}

	log.WithFields(logrus.Fields{
		"worker":   w.id,
		"event_id": notification.EventID,
		"dest":     notification.WebhookType,
	}).Info("Processing notification")

	// Send notification with retry logic
	if err := w.sendWithRetry(ctx, &notification); err != nil {
		log.Errorf("Failed to send notification after retries: %v", err)
		
		// Send to Dead Letter Queue
		if err := w.sendToDLQ(ctx, &notification, err); err != nil {
			log.Errorf("Failed to send to DLQ: %v", err)
		}
		dlqMessages.Inc()
	}

	// Acknowledge message
	msg.Ack()
}

func (w *Worker) sendWithRetry(ctx context.Context, notification *NotificationMessage) error {
	var lastErr error

	for attempt := 0; attempt <= w.config.MaxRetryAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter
			backoff := w.calculateBackoff(attempt)
			log.Infof("Retry attempt %d after %v", attempt, backoff)
			time.Sleep(backoff)
			retryAttempts.WithLabelValues(fmt.Sprintf("%d", attempt)).Inc()
		}

		// Record start time for latency metric
		startTime := time.Now()

		// Send notification
		err := w.notifier.Send(ctx, notification)
		
		// Record latency
		latency := time.Since(startTime).Seconds()
		notificationLatency.WithLabelValues(notification.WebhookType).Observe(latency)

		if err == nil {
			// Success
			notificationsSent.WithLabelValues(notification.WebhookType, "success").Inc()
			
			// Update database
			w.updateNotificationStatus(ctx, notification.EventID, "sent", "")
			
			log.WithFields(logrus.Fields{
				"event_id": notification.EventID,
				"attempt":  attempt,
				"latency":  latency,
			}).Info("Notification sent successfully")
			
			return nil
		}

		lastErr = err
		notificationsSent.WithLabelValues(notification.WebhookType, "retrying").Inc()
		
		log.WithFields(logrus.Fields{
			"event_id": notification.EventID,
			"attempt":  attempt,
			"error":    err.Error(),
		}).Warn("Notification send failed")

		// Update retry count in database
		w.updateNotificationStatus(ctx, notification.EventID, "retrying", err.Error())
	}

	// All retries exhausted
	notificationsSent.WithLabelValues(notification.WebhookType, "failed").Inc()
	w.updateNotificationStatus(ctx, notification.EventID, "failed", lastErr.Error())
	
	return fmt.Errorf("max retries exhausted: %w", lastErr)
}

func (w *Worker) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: 2^attempt seconds
	backoffSeconds := math.Pow(float64(w.config.RetryBackoffMultiplier), float64(attempt))
	
	// Cap at max backoff
	if backoffSeconds > float64(w.config.RetryMaxBackoffSeconds) {
		backoffSeconds = float64(w.config.RetryMaxBackoffSeconds)
	}

	// Add jitter (±20%)
	jitter := backoffSeconds * 0.2 * (rand.Float64()*2 - 1)
	backoff := time.Duration(backoffSeconds+jitter) * time.Second

	return backoff
}

func (w *Worker) sendToDLQ(ctx context.Context, notification *NotificationMessage, err error) error {
	dlqMessage := map[string]interface{}{
		"notification": notification,
		"error":        err.Error(),
		"timestamp":    time.Now(),
	}

	return w.queue.PublishToDLQ(ctx, "blockchain-events-dlq", dlqMessage)
}

func (w *Worker) updateNotificationStatus(ctx context.Context, eventID, status, errorMsg string) {
	if err := w.db.UpdateNotificationStatus(ctx, eventID, status, errorMsg); err != nil {
		log.Errorf("Failed to update notification status: %v", err)
	}
}

// NotifierPool manages different notification services
type NotifierPool struct {
	config     *Config
	db         Database
	cache      Cache
	cbManager  *CircuitBreakerManager
	httpClient *http.Client
}

func NewNotifierPool(config *Config, db Database, cache Cache, cbManager *CircuitBreakerManager) *NotifierPool {
	return &NotifierPool{
		config:    config,
		db:        db,
		cache:     cache,
		cbManager: cbManager,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (np *NotifierPool) Send(ctx context.Context, notification *NotificationMessage) error {
	// Check circuit breaker
	cb := np.cbManager.GetCircuitBreaker(notification.Destination)
	if !cb.CanExecute() {
		return fmt.Errorf("circuit breaker open for %s", notification.Destination)
	}

	var err error
	switch notification.WebhookType {
	case "discord":
		err = np.sendDiscord(ctx, notification)
	case "slack":
		err = np.sendSlack(ctx, notification)
	default:
		err = np.sendGenericWebhook(ctx, notification)
	}

	// Update circuit breaker
	if err != nil {
		cb.RecordFailure()
	} else {
		cb.RecordSuccess()
	}

	return err
}

func (np *NotifierPool) sendDiscord(ctx context.Context, notification *NotificationMessage) error {
	// Format Discord message
	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       fmt.Sprintf("🔔 New Event: %s", notification.Event.EventName),
				"description": fmt.Sprintf("Contract: `%s`\nBlock: %d", notification.Event.ContractAddress, notification.Event.BlockNumber),
				"color":       3447003, // Blue
				"fields": []map[string]interface{}{
					{
						"name":   "Transaction",
						"value":  fmt.Sprintf("[View on Etherscan](https://etherscan.io/tx/%s)", notification.Event.TxHash),
						"inline": false,
					},
					{
						"name":   "Chain ID",
						"value":  fmt.Sprintf("%d", notification.Event.ChainID),
						"inline": true,
					},
					{
						"name":   "Block Number",
						"value":  fmt.Sprintf("%d", notification.Event.BlockNumber),
						"inline": true,
					},
				},
				"timestamp": notification.Event.Timestamp.Format(time.RFC3339),
			},
		},
	}

	return np.sendWebhook(ctx, notification.Destination, payload)
}

func (np *NotifierPool) sendSlack(ctx context.Context, notification *NotificationMessage) error {
	// Format Slack message
	payload := map[string]interface{}{
		"blocks": []map[string]interface{}{
			{
				"type": "header",
				"text": map[string]interface{}{
					"type": "plain_text",
					"text": fmt.Sprintf("🔔 New Event: %s", notification.Event.EventName),
				},
			},
			{
				"type": "section",
				"fields": []map[string]interface{}{
					{
						"type": "mrkdwn",
						"text": fmt.Sprintf("*Contract:*\n`%s`", notification.Event.ContractAddress),
					},
					{
						"type": "mrkdwn",
						"text": fmt.Sprintf("*Block:*\n%d", notification.Event.BlockNumber),
					},
					{
						"type": "mrkdwn",
						"text": fmt.Sprintf("*Transaction:*\n<%s|View on Etherscan>", 
							fmt.Sprintf("https://etherscan.io/tx/%s", notification.Event.TxHash)),
					},
				},
			},
		},
	}

	return np.sendWebhook(ctx, notification.Destination, payload)
}

func (np *NotifierPool) sendGenericWebhook(ctx context.Context, notification *NotificationMessage) error {
	// Generic webhook payload
	payload := map[string]interface{}{
		"event_id":         notification.EventID,
		"chain_id":         notification.Event.ChainID,
		"block_number":     notification.Event.BlockNumber,
		"transaction_hash": notification.Event.TxHash,
		"contract_address": notification.Event.ContractAddress,
		"event_name":       notification.Event.EventName,
		"timestamp":        notification.Event.Timestamp,
	}

	return np.sendWebhook(ctx, notification.Destination, payload)
}

func (np *NotifierPool) sendWebhook(ctx context.Context, url string, payload interface{}) error {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Body = http.NoBody
	
	// Use bytes.NewReader for the actual payload
	import "bytes"
	req.Body = io.NopCloser(bytes.NewReader(jsonPayload))

	resp, err := np.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// CircuitBreaker prevents cascading failures
type CircuitBreaker struct {
	name              string
	threshold         int
	timeout           time.Duration
	failureCount      int
	lastFailureTime   time.Time
	state             CircuitBreakerState
}

type CircuitBreakerState int

const (
	StateClosed CircuitBreakerState = iota
	StateOpen
	StateHalfOpen
)

func NewCircuitBreaker(name string, threshold int, timeout time.Duration) *CircuitBreaker {
	cb := &CircuitBreaker{
		name:      name,
		threshold: threshold,
		timeout:   timeout,
		state:     StateClosed,
	}
	
	circuitBreakerState.WithLabelValues(name).Set(0)
	return cb
}

func (cb *CircuitBreaker) CanExecute() bool {
	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// Check if timeout has elapsed
		if time.Since(cb.lastFailureTime) > cb.timeout {
			cb.state = StateHalfOpen
			circuitBreakerState.WithLabelValues(cb.name).Set(2)
			log.Infof("Circuit breaker %s entering half-open state", cb.name)
			return true
		}
		return false
	case StateHalfOpen:
		return true
	}
	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	if cb.state == StateHalfOpen {
		cb.state = StateClosed
		cb.failureCount = 0
		circuitBreakerState.WithLabelValues(cb.name).Set(0)
		log.Infof("Circuit breaker %s closed", cb.name)
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.state == StateHalfOpen || cb.failureCount >= cb.threshold {
		cb.state = StateOpen
		circuitBreakerState.WithLabelValues(cb.name).Set(1)
		log.Warnf("Circuit breaker %s opened after %d failures", cb.name, cb.failureCount)
	}
}

// CircuitBreakerManager manages multiple circuit breakers
type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker
	config   *Config
}

func NewCircuitBreakerManager(config *Config) *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		config:   config,
	}
}

func (cbm *CircuitBreakerManager) GetCircuitBreaker(destination string) *CircuitBreaker {
	if cb, exists := cbm.breakers[destination]; exists {
		return cb
	}

	cb := NewCircuitBreaker(
		destination,
		cbm.config.CircuitBreakerThreshold,
		cbm.config.CircuitBreakerTimeout,
	)
	cbm.breakers[destination] = cb
	return cb
}

func startMetricsServer(port string) {
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf(":%s", port)
	log.Infof("Starting metrics server on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Metrics server failed: %v", err)
	}
}

// Helper functions and placeholder types
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

// Placeholder types
type Database interface {
	UpdateNotificationStatus(ctx context.Context, eventID, status, errorMsg string) error
	Close() error
}

type Cache interface {
	Close() error
}

type MessageQueue interface {
	Subscribe(ctx context.Context, topic string) (<-chan *QueueMessage, error)
	PublishToDLQ(ctx context.Context, topic string, message interface{}) error
	Close() error
}

type QueueMessage struct {
	Body []byte
	Ack  func()
}

type NotificationMessage struct {
	EventID     string `json:"event_id"`
	Event       *Event `json:"event"`
	Destination string `json:"destination"`
	WebhookType string `json:"webhook_type"`
}

type Event struct {
	ID              string    `json:"id"`
	ChainID         int64     `json:"chain_id"`
	BlockNumber     uint64    `json:"block_number"`
	TxHash          string    `json:"tx_hash"`
	LogIndex        uint      `json:"log_index"`
	ContractAddress string    `json:"contract_address"`
	EventName       string    `json:"event_name"`
	EventData       []byte    `json:"event_data"`
	Timestamp       time.Time `json:"timestamp"`
}

func initDatabase(url string) (Database, error) {
	log.Info("Initializing database connection")
	return nil, nil
}

func initRedis(url string) (Cache, error) {
	log.Info("Initializing Redis connection")
	return nil, nil
}

func initQueue(queueType, url string) (MessageQueue, error) {
	log.Infof("Initializing %s message queue", queueType)
	return nil, nil
}
