package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"on-chain-transaction-monitor/pkg/database"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
	logger = logrus.New()

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
			Help: "Time between current block and latest processed block",
		},
	)

	dbOperations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "db_operations_total",
			Help: "Total database operations",
		},
		[]string{"operation", "status"},
	)
)

func init() {
	prometheus.MustRegister(eventsIngested)
	prometheus.MustRegister(blockLag)
	prometheus.MustRegister(dbOperations)

	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetOutput(os.Stdout)
	
	level, _ := logrus.ParseLevel(getEnv("LOG_LEVEL", "info"))
	logger.SetLevel(level)
}

type Config struct {
	RPCPrimaryURL   string
	ChainID         int64
	MetricsPort     string
	DatabaseURL     string
}

func LoadConfig() *Config {
	return &Config{
		RPCPrimaryURL:  getEnv("RPC_PRIMARY_URL", ""),
		ChainID:        getEnvAsInt64("CHAIN_ID", 1),
		MetricsPort:    getEnv("METRICS_PORT_INGESTION", "8081"),
		DatabaseURL:    getEnv("DATABASE_URL", "postgresql://blockchain_user:blockchain_pass@localhost:5432/blockchain_monitor?sslmode=disable"),
	}
}

func main() {
	logger.Info("Starting On-Chain Transaction Monitor - Ingestion Service")

	config := LoadConfig()
	
	// Connect to database
	db, err := database.New(config.DatabaseURL)
	if err != nil {
		logger.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	logger.Info("✓ Connected to PostgreSQL database")

	go startMetricsServer(config.MetricsPort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Info("Blockchain client initialized (simulation mode with DB)")
	logger.Infof("Monitoring chain ID: %d", config.ChainID)

	go simulateEventProcessing(ctx, db, config)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	logger.Info("Shutting down gracefully...")
	cancel()
	time.Sleep(2 * time.Second)
	logger.Info("Shutdown complete")
}

func simulateEventProcessing(ctx context.Context, db *database.DB, config *Config) {
	logger.Info("Starting event simulation with database storage")
	
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	blockNumber := uint64(19000000)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			blockNumber++
			
			logger.WithFields(logrus.Fields{
				"block_number": blockNumber,
				"timestamp":    time.Now().Unix(),
			}).Debug("Processing simulated block")

			blockLag.Set(float64(2 + blockNumber%3))

			eventCount := blockNumber % 4
			for i := uint64(0); i < eventCount; i++ {
				if err := simulateEvent(ctx, db, config, blockNumber, i); err != nil {
					logger.Errorf("Failed to save event: %v", err)
					dbOperations.WithLabelValues("insert", "error").Inc()
				} else {
					dbOperations.WithLabelValues("insert", "success").Inc()
				}
			}
		}
	}
}

func simulateEvent(ctx context.Context, db *database.DB, config *Config, blockNumber, logIndex uint64) error {
	contracts := []string{
		"0xdAC17F958D2ee523a2206206994597C13D831ec7",
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2",
	}
	
	contract := contracts[blockNumber%3]
	txHash := fmt.Sprintf("0x%x", blockNumber*1000+logIndex)
	eventID := fmt.Sprintf("%d-%s-%d", config.ChainID, txHash, logIndex)
	
	eventData := map[string]interface{}{
		"from":   "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
		"to":     "0x8888887dD47e5C1234567890AbcdEF1234567890",
		"value":  "1000000000000000000",
	}
	
	eventDataJSON, _ := json.Marshal(eventData)
	
	event := &database.Event{
		EventID:         eventID,
		ChainID:         config.ChainID,
		BlockNumber:     blockNumber,
		TxHash:          txHash,
		LogIndex:        uint(logIndex),
		ContractAddress: contract,
		EventName:       "Transfer",
		EventData:       eventDataJSON,
	}
	
	if err := db.SaveEvent(ctx, event); err != nil {
		return err
	}
	
	eventsIngested.WithLabelValues(contract, "Transfer").Inc()
	
	logger.WithFields(logrus.Fields{
		"event_id":  eventID,
		"contract":  contract,
		"block":     blockNumber,
	}).Info("Event saved to database")
	
	return nil
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

func getEnvAsInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		var result int64
		fmt.Sscanf(value, "%d", &result)
		return result
	}
	return defaultValue
}
