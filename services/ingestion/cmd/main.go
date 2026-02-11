package main

import (
	"context"
	"fmt"
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
	log = logrus.New()

	// Prometheus metrics
	eventsIngested = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "events_ingested_total",
			Help: "Total number of events ingested",
		},
		[]string{"contract", "event_type"},
	)

	blockLag = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "block_lag_seconds",
			Help: "Time difference between current block and latest processed block",
		},
	)

	rpcErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rpc_errors_total",
			Help: "Total number of RPC errors",
		},
		[]string{"error_type"},
	)

	websocketReconnections = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "websocket_reconnections_total",
			Help: "Total number of WebSocket reconnections",
		},
	)
)

func init() {
	// Register metrics
	prometheus.MustRegister(eventsIngested)
	prometheus.MustRegister(blockLag)
	prometheus.MustRegister(rpcErrors)
	prometheus.MustRegister(websocketReconnections)

	// Configure logging
	log.SetFormatter(&logrus.JSONFormatter{})
	log.SetOutput(os.Stdout)
	
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	log.SetLevel(level)
}

// Config holds application configuration
type Config struct {
	RPCPrimaryURL   string
	RPCFallbackURL  string
	ChainID         int64
	StartBlock      string
	MetricsPort     string
	DatabaseURL     string
	RedisURL        string
	QueueURL        string
	QueueType       string // "rabbitmq" or "kafka"
}

// LoadConfig loads configuration from environment variables
func LoadConfig() *Config {
	return &Config{
		RPCPrimaryURL:  getEnv("RPC_PRIMARY_URL", ""),
		RPCFallbackURL: getEnv("RPC_FALLBACK_URL", ""),
		ChainID:        getEnvAsInt64("CHAIN_ID", 1),
		StartBlock:     getEnv("START_BLOCK", "latest"),
		MetricsPort:    getEnv("METRICS_PORT", "8081"),
		DatabaseURL:    getEnv("DATABASE_URL", ""),
		RedisURL:       getEnv("REDIS_URL", ""),
		QueueURL:       getEnv("RABBITMQ_URL", ""),
		QueueType:      getEnv("MESSAGE_QUEUE_TYPE", "rabbitmq"),
	}
}

func main() {
	log.Info("Starting Blockchain Ingestion Service")

	// Load configuration
	config := LoadConfig()
	
	if config.RPCPrimaryURL == "" {
		log.Fatal("RPC_PRIMARY_URL is required")
	}

	// Start metrics server
	go startMetricsServer(config.MetricsPort)

	// Create context for graceful shutdown
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

	// Initialize blockchain client with automatic reconnection
	client := NewBlockchainClient(config, queue, db, cache)
	
	// Start event ingestion
	go func() {
		if err := client.Start(ctx); err != nil {
			log.Errorf("Ingestion error: %v", err)
			cancel()
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Info("Received shutdown signal")
	case <-ctx.Done():
		log.Info("Context cancelled")
	}

	// Graceful shutdown
	log.Info("Shutting down gracefully...")
	cancel()
	time.Sleep(5 * time.Second) // Allow in-flight requests to complete
	log.Info("Shutdown complete")
}

// BlockchainClient manages WebSocket connection and event processing
type BlockchainClient struct {
	config       *Config
	queue        MessageQueue
	db           Database
	cache        Cache
	rpcProvider  RPCProvider
	isConnected  bool
	lastBlock    uint64
}

func NewBlockchainClient(config *Config, queue MessageQueue, db Database, cache Cache) *BlockchainClient {
	return &BlockchainClient{
		config:      config,
		queue:       queue,
		db:          db,
		cache:       cache,
		isConnected: false,
	}
}

func (c *BlockchainClient) Start(ctx context.Context) error {
	log.Info("Starting blockchain client")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			if err := c.connectAndListen(ctx); err != nil {
				log.Errorf("Connection error: %v", err)
				rpcErrors.WithLabelValues("connection_failed").Inc()
				
				// Try fallback provider
				if c.config.RPCFallbackURL != "" {
					log.Info("Attempting fallback RPC provider")
					if err := c.connectToFallback(ctx); err != nil {
						log.Errorf("Fallback connection failed: %v", err)
					}
				}
				
				// Exponential backoff before reconnect
				backoff := time.Second * 5
				log.Infof("Reconnecting in %v", backoff)
				time.Sleep(backoff)
				websocketReconnections.Inc()
			}
		}
	}
}

func (c *BlockchainClient) connectAndListen(ctx context.Context) error {
	// Create WebSocket connection
	provider, err := NewRPCProvider(c.config.RPCPrimaryURL)
	if err != nil {
		return fmt.Errorf("failed to create RPC provider: %w", err)
	}
	defer provider.Close()

	c.rpcProvider = provider
	c.isConnected = true

	log.Info("Connected to blockchain RPC")

	// Subscribe to new blocks
	blockChan := make(chan *Block, 100)
	sub, err := provider.SubscribeNewBlocks(ctx, blockChan)
	if err != nil {
		return fmt.Errorf("failed to subscribe to blocks: %w", err)
	}
	defer sub.Unsubscribe()

	// Process blocks
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-sub.Err():
			return fmt.Errorf("subscription error: %w", err)
		case block := <-blockChan:
			if err := c.processBlock(ctx, block); err != nil {
				log.Errorf("Failed to process block %d: %v", block.Number, err)
			}
		}
	}
}

func (c *BlockchainClient) processBlock(ctx context.Context, block *Block) error {
	log.WithFields(logrus.Fields{
		"block_number": block.Number,
		"tx_count":     len(block.Transactions),
	}).Debug("Processing block")

	// Update block lag metric
	blockTime := time.Unix(int64(block.Timestamp), 0)
	lag := time.Since(blockTime).Seconds()
	blockLag.Set(lag)

	// Get subscribed contracts from database
	subscriptions, err := c.db.GetActiveSubscriptions()
	if err != nil {
		return fmt.Errorf("failed to get subscriptions: %w", err)
	}

	// Process events in block
	for _, tx := range block.Transactions {
		receipt, err := c.rpcProvider.GetTransactionReceipt(ctx, tx.Hash)
		if err != nil {
			log.Warnf("Failed to get receipt for tx %s: %v", tx.Hash, err)
			continue
		}

		for _, log := range receipt.Logs {
			// Check if this log matches any subscription
			for _, sub := range subscriptions {
				if sub.MatchesLog(log) {
					if err := c.handleEvent(ctx, block, tx, log, sub); err != nil {
						log.Errorf("Failed to handle event: %v", err)
					}
				}
			}
		}
	}

	c.lastBlock = block.Number
	return nil
}

func (c *BlockchainClient) handleEvent(ctx context.Context, block *Block, tx *Transaction, eventLog *Log, sub *Subscription) error {
	// Create unique event ID for idempotency
	eventID := fmt.Sprintf("%d-%s-%d", c.config.ChainID, tx.Hash, eventLog.Index)

	// Check if already processed (idempotency)
	exists, err := c.cache.Exists(ctx, fmt.Sprintf("event:%s", eventID))
	if err != nil {
		return fmt.Errorf("cache check failed: %w", err)
	}
	if exists {
		log.Debugf("Event %s already processed, skipping", eventID)
		return nil
	}

	// Parse event data
	event := &Event{
		ID:              eventID,
		ChainID:         c.config.ChainID,
		BlockNumber:     block.Number,
		TxHash:          tx.Hash,
		LogIndex:        eventLog.Index,
		ContractAddress: eventLog.Address,
		EventName:       sub.EventName,
		EventData:       eventLog.Data,
		Timestamp:       time.Now(),
	}

	// Store in database
	if err := c.db.SaveEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to save event: %w", err)
	}

	// Publish to message queue
	message := &NotificationMessage{
		EventID:     eventID,
		Event:       event,
		Destination: sub.WebhookURL,
		WebhookType: sub.WebhookType,
	}

	if err := c.queue.Publish(ctx, "blockchain-events", message); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	// Mark as processed in cache (24h TTL)
	if err := c.cache.Set(ctx, fmt.Sprintf("event:%s", eventID), "1", 24*time.Hour); err != nil {
		log.Warnf("Failed to cache event: %v", err)
	}

	// Update metrics
	eventsIngested.WithLabelValues(event.ContractAddress, event.EventName).Inc()

	log.WithFields(logrus.Fields{
		"event_id":  eventID,
		"contract":  event.ContractAddress,
		"event":     event.EventName,
		"block":     block.Number,
	}).Info("Event ingested successfully")

	return nil
}

func (c *BlockchainClient) connectToFallback(ctx context.Context) error {
	provider, err := NewRPCProvider(c.config.RPCFallbackURL)
	if err != nil {
		return err
	}
	
	c.rpcProvider = provider
	log.Info("Connected to fallback RPC provider")
	return nil
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

// Helper functions
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		var result int64
		fmt.Sscanf(value, "%d", &result)
		return result
	}
	return defaultValue
}

// Placeholder types - implement these in separate files
type Database interface {
	GetActiveSubscriptions() ([]*Subscription, error)
	SaveEvent(ctx context.Context, event *Event) error
	Close() error
}

type Cache interface {
	Exists(ctx context.Context, key string) (bool, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Close() error
}

type MessageQueue interface {
	Publish(ctx context.Context, topic string, message interface{}) error
	Close() error
}

type RPCProvider interface {
	SubscribeNewBlocks(ctx context.Context, ch chan *Block) (Subscription, error)
	GetTransactionReceipt(ctx context.Context, txHash string) (*TransactionReceipt, error)
	Close() error
}

type Subscription struct {
	ContractAddress string
	EventName       string
	WebhookURL      string
	WebhookType     string
	Filters         map[string]interface{}
}

func (s *Subscription) MatchesLog(log *Log) bool {
	// Implement matching logic based on contract address and event signature
	return s.ContractAddress == log.Address
}

type Event struct {
	ID              string
	ChainID         int64
	BlockNumber     uint64
	TxHash          string
	LogIndex        uint
	ContractAddress string
	EventName       string
	EventData       []byte
	Timestamp       time.Time
}

type NotificationMessage struct {
	EventID     string
	Event       *Event
	Destination string
	WebhookType string
}

type Block struct {
	Number       uint64
	Hash         string
	Timestamp    uint64
	Transactions []*Transaction
}

type Transaction struct {
	Hash string
	From string
	To   string
}

type TransactionReceipt struct {
	TxHash string
	Logs   []*Log
	Status uint64
}

type Log struct {
	Address string
	Topics  []string
	Data    []byte
	Index   uint
}

func initDatabase(url string) (Database, error) {
	// Implement PostgreSQL connection
	log.Info("Initializing database connection")
	return nil, nil // Placeholder
}

func initRedis(url string) (Cache, error) {
	// Implement Redis connection
	log.Info("Initializing Redis connection")
	return nil, nil // Placeholder
}

func initQueue(queueType, url string) (MessageQueue, error) {
	// Implement RabbitMQ or Kafka connection
	log.Infof("Initializing %s message queue", queueType)
	return nil, nil // Placeholder
}

func NewRPCProvider(url string) (RPCProvider, error) {
	// Implement WebSocket RPC provider
	log.Infof("Creating RPC provider: %s", url)
	return nil, nil // Placeholder
}
