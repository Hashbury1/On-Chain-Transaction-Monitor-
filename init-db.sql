-- Create tables for the blockchain event monitoring system

-- Events table
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

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_events_contract ON events(contract_address);
CREATE INDEX IF NOT EXISTS idx_events_block ON events(block_number);
CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at);
CREATE INDEX IF NOT EXISTS idx_events_event_name ON events(event_name);

-- Notifications table
CREATE TABLE IF NOT EXISTS notifications (
    id SERIAL PRIMARY KEY,
    event_id VARCHAR(255) REFERENCES events(event_id),
    destination VARCHAR(255) NOT NULL,
    webhook_type VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    retry_count INTEGER DEFAULT 0,
    last_error TEXT,
    sent_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Indexes for notifications
CREATE INDEX IF NOT EXISTS idx_notifications_status ON notifications(status);
CREATE INDEX IF NOT EXISTS idx_notifications_event ON notifications(event_id);
CREATE INDEX IF NOT EXISTS idx_notifications_created ON notifications(created_at);

-- Subscriptions table
CREATE TABLE IF NOT EXISTS subscriptions (
    id SERIAL PRIMARY KEY,
    contract_address VARCHAR(42) NOT NULL,
    event_name VARCHAR(255),
    webhook_url TEXT NOT NULL,
    webhook_type VARCHAR(20) NOT NULL,
    filters JSONB,
    active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Indexes for subscriptions
CREATE INDEX IF NOT EXISTS idx_subscriptions_contract ON subscriptions(contract_address);
CREATE INDEX IF NOT EXISTS idx_subscriptions_active ON subscriptions(active);

-- Insert some test subscriptions
INSERT INTO subscriptions (contract_address, event_name, webhook_url, webhook_type, active) 
VALUES 
    ('0xdAC17F958D2ee523a2206206994597C13D831ec7', 'Transfer', 'https://discord.com/api/webhooks/test', 'discord', true),
    ('0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48', 'Transfer', 'https://hooks.slack.com/services/test', 'slack', true)
ON CONFLICT DO NOTHING;

-- Grant permissions
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO Onchain_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO Onchain_user;