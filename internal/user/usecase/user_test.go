package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/piresc/go-project/internal/user/domain"
)

type userRepo struct {
	user         *domain.User
	updateCalled bool
}

func (r *userRepo) FindByID(context.Context, string) (*domain.User, error) { return r.user, nil }
func (r *userRepo) FindByEmail(context.Context, string) (*domain.User, error) {
	return nil, nil
}
func (r *userRepo) Create(context.Context, *domain.User) error { return nil }
func (r *userRepo) UpdateDisplayName(_ context.Context, _ string, name string) error {
	r.updateCalled = true
	r.user.DisplayName = name
	return nil
}
func (r *userRepo) FindByProvider(context.Context, string, string) (*domain.User, error) {
	return nil, nil
}
func (r *userRepo) CreateAuthProvider(context.Context, *domain.AuthProvider) error { return nil }
func (r *userRepo) CreateRefreshToken(context.Context, *domain.RefreshToken) error { return nil }
func (r *userRepo) FindRefreshTokenByHash(context.Context, string) (*domain.RefreshToken, error) {
	return nil, nil
}
func (r *userRepo) RevokeRefreshToken(context.Context, string) error         { return nil }
func (r *userRepo) RevokeAllUserRefreshTokens(context.Context, string) error { return nil }

type userCache struct {
	user          *domain.User
	deletedUserID string
	getErr        error
}

func (c *userCache) GetUser(context.Context, string) (*domain.User, bool, error) {
	if c.getErr != nil {
		return nil, false, c.getErr
	}
	return c.user, c.user != nil, nil
}
func (c *userCache) SetUser(_ context.Context, user *domain.User) error {
	c.user = user
	return nil
}
func (c *userCache) DeleteUser(_ context.Context, userID string) error {
	c.deletedUserID = userID
	c.user = nil
	return nil
}

func TestGetProfileUsesCacheAside(t *testing.T) {
	repo := &userRepo{user: &domain.User{ID: "user-1", DisplayName: "Test User"}}
	cache := &userCache{}
	uc := NewUserUseCase(repo, cache, discardLogger())

	user, err := uc.GetProfile(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("GetProfile returned error: %v", err)
	}
	if user.ID != "user-1" || cache.user == nil {
		t.Fatalf("expected cached user after load, got user=%+v cache=%+v", user, cache.user)
	}
}

func TestGetProfile_CacheErrorFallsBackToRepo(t *testing.T) {
	repo := &userRepo{user: &domain.User{ID: "user-1", DisplayName: "Test User"}}
	cache := &userCache{getErr: errors.New("redis down")}
	uc := NewUserUseCase(repo, cache, discardLogger())

	user, err := uc.GetProfile(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("GetProfile returned error: %v", err)
	}
	if user.ID != "user-1" {
		t.Fatalf("expected profile from repo, got %+v", user)
	}
}

func TestGetProfile_NotFound(t *testing.T) {
	uc := NewUserUseCase(&userRepo{}, &userCache{}, discardLogger())
	if _, err := uc.GetProfile(context.Background(), "missing"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUpdateProfileInvalidatesCache(t *testing.T) {
	repo := &userRepo{user: &domain.User{ID: "user-1", DisplayName: "Old"}}
	cache := &userCache{user: repo.user}
	uc := NewUserUseCase(repo, cache, discardLogger())

	updated, err := uc.UpdateProfile(context.Background(), "user-1", domain.ProfileUpdate{DisplayName: stringPtr("New")})
	if err != nil {
		t.Fatalf("UpdateProfile returned error: %v", err)
	}
	if updated.DisplayName != "New" || cache.deletedUserID != "user-1" {
		t.Fatalf("expected updated profile and cache invalidation, got user=%+v cacheDeleted=%s", updated, cache.deletedUserID)
	}
}

func TestUpdateProfile_TrimsDisplayName(t *testing.T) {
	repo := &userRepo{user: &domain.User{ID: "user-1", DisplayName: "Old"}}
	uc := NewUserUseCase(repo, &userCache{}, discardLogger())

	updated, err := uc.UpdateProfile(context.Background(), "user-1", domain.ProfileUpdate{DisplayName: stringPtr("  New  ")})
	if err != nil {
		t.Fatalf("UpdateProfile returned error: %v", err)
	}
	if updated.DisplayName != "New" {
		t.Fatalf("expected trimmed name, got %q", updated.DisplayName)
	}
}

func TestUpdateProfile_BlankNameRejected(t *testing.T) {
	repo := &userRepo{user: &domain.User{ID: "user-1", DisplayName: "Old"}}
	uc := NewUserUseCase(repo, &userCache{}, discardLogger())

	_, err := uc.UpdateProfile(context.Background(), "user-1", domain.ProfileUpdate{DisplayName: stringPtr("   ")})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if repo.updateCalled {
		t.Fatal("expected nothing written")
	}
}

func TestUpdateProfile_NilNameRejected(t *testing.T) {
	repo := &userRepo{user: &domain.User{ID: "user-1", DisplayName: "Old"}}
	uc := NewUserUseCase(repo, &userCache{}, discardLogger())

	_, err := uc.UpdateProfile(context.Background(), "user-1", domain.ProfileUpdate{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if repo.updateCalled {
		t.Fatal("expected nothing written")
	}
}

func TestUpdateProfile_NameLengthByRunes(t *testing.T) {
	repo := &userRepo{user: &domain.User{ID: "user-1", DisplayName: "Old"}}
	uc := NewUserUseCase(repo, &userCache{}, discardLogger())

	// 100 multi-byte runes is accepted (300 bytes).
	if _, err := uc.UpdateProfile(context.Background(), "user-1", domain.ProfileUpdate{DisplayName: stringPtr(strings.Repeat("é", 100))}); err != nil {
		t.Fatalf("expected 100 runes accepted, got %v", err)
	}

	repo.updateCalled = false
	_, err := uc.UpdateProfile(context.Background(), "user-1", domain.ProfileUpdate{DisplayName: stringPtr(strings.Repeat("a", 101))})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for 101 runes, got %v", err)
	}
	if repo.updateCalled {
		t.Fatal("expected nothing written")
	}
}

func stringPtr(value string) *string { return &value }
