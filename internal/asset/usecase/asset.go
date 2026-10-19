// Package usecase holds asset-service business logic.
package usecase

import (
	"context"

	"github.com/piresc/go-project/internal/asset/domain"
)

// AssetUsecase orchestrates asset operations. Placeholder: no logic yet.
type AssetUsecase struct {
	repo AssetRepository
}

// NewAssetUsecase builds an AssetUsecase on top of its repository port.
func NewAssetUsecase(repo AssetRepository) *AssetUsecase {
	return &AssetUsecase{repo: repo}
}

// List returns the tracked assets. Placeholder: always empty.
func (u *AssetUsecase) List(context.Context) ([]domain.Asset, error) {
	return []domain.Asset{}, nil
}
