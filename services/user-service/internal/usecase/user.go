package usecase

import (
	"context"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/piresc/go-project/services/user-service/internal/domain"
)

const maxDisplayNameRunes = 100

// UserUseCase owns user profile reads and writes.
type UserUseCase struct {
	repo  UserRepository
	cache UserCache
	log   *slog.Logger
}

// NewUserUseCase constructs the user profile use case.
func NewUserUseCase(repo UserRepository, cache UserCache, log *slog.Logger) *UserUseCase {
	return &UserUseCase{
		repo:  repo,
		cache: cache,
		log:   log,
	}
}

// GetProfile loads a user profile with cache-aside reads.
func (uc *UserUseCase) GetProfile(ctx context.Context, userID string) (*domain.User, error) {
	user, found, err := uc.cache.GetUser(ctx, userID)
	if err == nil && found {
		return user, nil
	}
	if err != nil {
		uc.log.WarnContext(ctx, "cache lookup failed", "user_id", userID, "error", err)
	}

	user, err = uc.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}

	if err := uc.cache.SetUser(ctx, user); err != nil {
		uc.log.WarnContext(ctx, "failed to cache user", "user_id", userID, "error", err)
	}
	return user, nil
}

// UpdateProfile updates the user's display name and returns the fresh profile.
// The display name is required, trimmed, and limited to 100 runes.
func (uc *UserUseCase) UpdateProfile(ctx context.Context, userID string, update domain.ProfileUpdate) (*domain.User, error) {
	if update.DisplayName == nil {
		return nil, ErrInvalidInput
	}
	name := strings.TrimSpace(*update.DisplayName)
	if name == "" || utf8.RuneCountInString(name) > maxDisplayNameRunes {
		return nil, ErrInvalidInput
	}

	if err := uc.repo.UpdateDisplayName(ctx, userID, name); err != nil {
		return nil, err
	}

	if err := uc.cache.DeleteUser(ctx, userID); err != nil {
		uc.log.WarnContext(ctx, "failed to invalidate user cache", "user_id", userID, "error", err)
	}

	user, err := uc.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}
