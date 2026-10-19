CREATE TABLE IF NOT EXISTS holdings (
    user_id    UUID NOT NULL REFERENCES portfolios(user_id) ON DELETE CASCADE,
    symbol     TEXT NOT NULL,
    quantity   DOUBLE PRECISION NOT NULL CHECK (quantity > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, symbol)
);
