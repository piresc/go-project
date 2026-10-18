// Package usecase holds asset-service business logic.
package usecase

// AssetUsecase orchestrates asset operations. Placeholder: no logic yet.
type AssetUsecase struct {
	repo AssetRepository
}

// NewAssetUsecase builds an AssetUsecase on top of its repository port.
func NewAssetUsecase(repo AssetRepository) *AssetUsecase {
	return &AssetUsecase{repo: repo}
}
