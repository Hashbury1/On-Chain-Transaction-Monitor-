#!/bin/bash

set -e

DIRECTION=${1:-up}

echo "Running database migrations: $DIRECTION"

# Add migration logic here
# For now, just create basic schema

if [ "$DIRECTION" = "up" ]; then
    psql $DATABASE_URL << 'SQL'
    CREATE TABLE IF NOT EXISTS events (
        id SERIAL PRIMARY KEY,
        event_id VARCHAR(255) UNIQUE NOT NULL,
        chain_id INTEGER NOT NULL,
        block_number BIGINT NOT NULL,
        tx_hash VARCHAR(66) NOT NULL,
        log_index INTEGER NOT NULL,
        contract_address VARCHAR(42) NOT NULL,
        event_name VARCHAR(255) NOT NULL,
        event_data JSONB NOT NULL,
        processed_at TIMESTAMP DEFAULT NOW(),
        created_at TIMESTAMP DEFAULT NOW()
    );

    CREATE INDEX idx_events_contract ON events(contract_address);
    CREATE INDEX idx_events_block ON events(block_number);
    CREATE INDEX idx_events_created ON events(created_at);

    CREATE TABLE IF NOT EXISTS notifications (
        id SERIAL PRIMARY KEY,
        event_id VARCHAR(255) REFERENCES events(event_id),
        destination VARCHAR(50) NOT NULL,
        status VARCHAR(20) NOT NULL,
        retry_count INTEGER DEFAULT 0,
        last_error TEXT,
        sent_at TIMESTAMP,
        created_at TIMESTAMP DEFAULT NOW()
    );

    CREATE INDEX idx_notifications_status ON notifications(status);
    CREATE INDEX idx_notifications_event ON notifications(event_id);

    CREATE TABLE IF NOT EXISTS subscriptions (
        id SERIAL PRIMARY KEY,
        contract_address VARCHAR(42) NOT NULL,
        event_name VARCHAR(255),
        webhook_url TEXT NOT NULL,
        webhook_type VARCHAR(20) NOT NULL,
        filters JSONB,
        active BOOLEAN DEFAULT true,
        created_at TIMESTAMP DEFAULT NOW()
    );

    CREATE INDEX idx_subscriptions_contract ON subscriptions(contract_address);
SQL

    echo "✅ Migrations applied successfully"
else
    echo "⚠️  Migration rollback not implemented yet"
fi
