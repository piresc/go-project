// Package postgres implements usecase repository ports on PostgreSQL.
package postgres

import "github.com/jmoiron/sqlx"

// AssetRepository stores assets in Postgres. Placeholder: no queries yet.
type AssetRepository struct {
	db *sqlx.DB
}

// NewAssetRepository builds an AssetRepository on the given connection.
func NewAssetRepository(db *sqlx.DB) *AssetRepository {
	return &AssetRepository{db: db}
}
