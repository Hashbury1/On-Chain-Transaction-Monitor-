package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

type DB struct {
	conn *sql.DB
}

type Event struct {
	ID              int
	EventID         string
	ChainID         int64
	BlockNumber     uint64
	TxHash          string
	LogIndex        uint
	ContractAddress string
	EventName       string
	EventData       []byte
	ProcessedAt     time.Time
	CreatedAt       time.Time
}

type Subscription struct {
	ID              int
	ContractAddress string
	EventName       string
	WebhookURL      string
	WebhookType     string
	Filters         json.RawMessage
	Active          bool
	CreatedAt       time.Time
}

func New(connectionString string) (*DB, error) {
	conn, err := sql.Open("postgres", connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(5 * time.Minute)

	return &DB{conn: conn}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) SaveEvent(ctx context.Context, event *Event) error {
	query := `
		INSERT INTO events (event_id, chain_id, block_number, tx_hash, log_index, 
		                   contract_address, event_name, event_data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (event_id) DO NOTHING
	`
	_, err := db.conn.ExecContext(ctx, query,
		event.EventID, event.ChainID, event.BlockNumber, event.TxHash,
		event.LogIndex, event.ContractAddress, event.EventName, event.EventData,
	)
	return err
}

func (db *DB) GetActiveSubscriptions(ctx context.Context) ([]*Subscription, error) {
	query := `
		SELECT id, contract_address, event_name, webhook_url, webhook_type, 
		       filters, active, created_at
		FROM subscriptions
		WHERE active = true
	`
	rows, err := db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []*Subscription
	for rows.Next() {
		var s Subscription
		err := rows.Scan(&s.ID, &s.ContractAddress, &s.EventName, &s.WebhookURL,
			&s.WebhookType, &s.Filters, &s.Active, &s.CreatedAt)
		if err != nil {
			return nil, err
		}
		subs = append(subs, &s)
	}
	return subs, nil
}

func (db *DB) UpdateNotificationStatus(ctx context.Context, eventID, status, errorMsg string) error {
	query := `
		UPDATE notifications
		SET status = $1, last_error = $2, sent_at = NOW()
		WHERE event_id = $3
	`
	_, err := db.conn.ExecContext(ctx, query, status, errorMsg, eventID)
	return err
}
