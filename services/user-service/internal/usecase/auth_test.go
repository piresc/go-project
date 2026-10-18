package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/piresc/go-project/services/user-service/internal/domain"
)

const testAccessTTL = 15 * time.Minute

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type authRepo struct {
	users         map[string]*domain.User
	providers     []*domain.AuthProvider
	refreshTokens map[string]*domain.RefreshToken
	nextTokenID   int
}

func newAuthRepo() *authRepo {
	return &authRepo{
		users:         make(map[string]*domain.User),
		refreshTokens: make(map[string]*domain.RefreshToken),
	}
}

func (r *authRepo) FindByID(_ context.Context, id string) (*domain.User, error) {
	return r.users[id], nil
}
func (r *authRepo) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	for _, user := range r.users {
		if user.Email == email {
			return user, nil
		}
	}
	return nil, nil
}
func (r *authRepo) Create(_ context.Context, user *domain.User) error {
	if user.ID == "" {
		user.ID = "user-1"
	}
	r.users[user.ID] = user
	return nil
}
func (r *authRepo) UpdateDisplayName(context.Context, string, string) error { return nil }
func (r *authRepo) FindByProvider(_ context.Context, provider, subject string) (*domain.User, error) {
	for _, ap := range r.providers {
		if ap.Provider == provider && ap.ProviderSubject == subject {
			return r.users[ap.UserID], nil
		}
	}
	return nil, nil
}
func (r *authRepo) CreateAuthProvider(_ context.Context, provider *domain.AuthProvider) error {
	r.providers = append(r.providers, provider)
	return nil
}
func (r *authRepo) CreateRefreshToken(_ context.Context, token *domain.RefreshToken) error {
	if token.ID == "" {
		r.nextTokenID++
		token.ID = "token-" + strconv.Itoa(r.nextTokenID)
	}
	r.refreshTokens[token.TokenHash] = token
	return nil
}
func (r *authRepo) FindRefreshTokenByHash(_ context.Context, hash string) (*domain.RefreshToken, error) {
	return r.refreshTokens[hash], nil
}
func (r *authRepo) RevokeRefreshToken(_ context.Context, id string) error {
	for _, token := range r.refreshTokens {
		if token.ID == id {
			token.Revoked = true
		}
	}
	return nil
}
func (r *authRepo) RevokeAllUserRefreshTokens(_ context.Context, userID string) error {
	for _, token := range r.refreshTokens {
		if token.UserID == userID {
			token.Revoked = true
		}
	}
	return nil
}

type authCache struct{}

func (c *authCache) GetUser(context.Context, string) (*domain.User, bool, error) {
	return nil, false, nil
}
func (c *authCache) SetUser(context.Context, *domain.User) error { return nil }
func (c *authCache) DeleteUser(context.Context, string) error    { return nil }

type fakeRevoker struct {
	jti   string
	ttl   time.Duration
	calls int
	err   error
}

func (r *fakeRevoker) Revoke(_ context.Context, jti string, ttl time.Duration) error {
	r.calls++
	r.jti = jti
	r.ttl = ttl
	return r.err
}

type authPublisher struct {
	events []domain.UserCreatedEvent
}

func (p *authPublisher) PublishUserCreated(_ context.Context, event domain.UserCreatedEvent) error {
	p.events = append(p.events, event)
	return nil
}

type authVerifier struct {
	claims *domain.OAuthIdentity
}

func (v *authVerifier) Verify(context.Context, string) (*domain.OAuthIdentity, error) {
	return v.claims, nil
}

// authTokenService issues a distinct refresh token on every call.
type authTokenService struct {
	n int
}

func (s *authTokenService) GenerateAccessToken(user *domain.User) (string, string, time.Time, error) {
	return "access-" + user.ID, "jti-1", time.Now().Add(testAccessTTL), nil
}
func (s *authTokenService) GenerateRefreshToken() (string, string, time.Time, error) {
	s.n++
	plaintext := fmt.Sprintf("refresh-%d", s.n)
	sum := sha256.Sum256([]byte(plaintext))
	return plaintext, hex.EncodeToString(sum[:]), time.Now().Add(24 * time.Hour), nil
}

func newTestAuth(repo *authRepo, revoker TokenRevoker, verifier *authVerifier, pub *authPublisher) *AuthUseCase {
	return NewAuthUseCase(repo, &authCache{}, revoker, pub, verifier, verifier, &authTokenService{}, testAccessTTL, discardLogger())
}

func TestAuthenticateGoogleCreatesUserPublishesEventAndStoresRefreshToken(t *testing.T) {
	repo := newAuthRepo()
	publisher := &authPublisher{}
	verifier := &authVerifier{claims: &domain.OAuthIdentity{
		Subject:  "google-subject",
		Email:    "test@example.com",
		Name:     "Test User",
		Provider: "google",
	}}
	uc := newTestAuth(repo, &fakeRevoker{}, verifier, publisher)

	tokenPair, err := uc.AuthenticateGoogle(context.Background(), "id-token")
	if err != nil {
		t.Fatalf("AuthenticateGoogle returned error: %v", err)
	}
	if tokenPair.AccessToken == "" || tokenPair.RefreshToken == "" {
		t.Fatalf("expected token pair, got %+v", tokenPair)
	}
	if tokenPair.ExpiresIn != int(testAccessTTL.Seconds()) {
		t.Fatalf("expected ExpiresIn %d, got %d", int(testAccessTTL.Seconds()), tokenPair.ExpiresIn)
	}
	if len(repo.users) != 1 || len(publisher.events) != 1 || len(repo.refreshTokens) != 1 {
		t.Fatalf("expected user creation side effects, got users=%d events=%d refresh=%d", len(repo.users), len(publisher.events), len(repo.refreshTokens))
	}
}

func TestAuthenticateAppleLinksProviderToExistingEmailUser(t *testing.T) {
	repo := newAuthRepo()
	repo.users["user-9"] = &domain.User{ID: "user-9", Email: "test@example.com", Role: "investor"}
	publisher := &authPublisher{}
	verifier := &authVerifier{claims: &domain.OAuthIdentity{
		Subject:  "apple-subject",
		Email:    "test@example.com",
		Provider: "apple",
	}}
	uc := newTestAuth(repo, &fakeRevoker{}, verifier, publisher)

	if _, err := uc.AuthenticateApple(context.Background(), "id-token"); err != nil {
		t.Fatalf("AuthenticateApple returned error: %v", err)
	}
	if len(repo.users) != 1 || len(publisher.events) != 0 {
		t.Fatalf("expected no new user or event, got users=%d events=%d", len(repo.users), len(publisher.events))
	}
	if len(repo.providers) != 1 || repo.providers[0].UserID != "user-9" {
		t.Fatalf("expected provider linked to user-9, got %+v", repo.providers)
	}
}

func TestRefreshTokensRevokesOldToken(t *testing.T) {
	repo := newAuthRepo()
	repo.users["user-1"] = &domain.User{ID: "user-1", Email: "test@example.com", Role: "investor"}
	hash := sha256.Sum256([]byte("refresh"))
	hashString := hex.EncodeToString(hash[:])
	repo.refreshTokens[hashString] = &domain.RefreshToken{
		ID:        "token-1",
		UserID:    "user-1",
		TokenHash: hashString,
		ExpiresAt: time.Now().Add(time.Hour),
	}

	uc := newTestAuth(repo, &fakeRevoker{}, &authVerifier{}, &authPublisher{})
	tokenPair, err := uc.RefreshTokens(context.Background(), "refresh")
	if err != nil {
		t.Fatalf("RefreshTokens returned error: %v", err)
	}
	if tokenPair.AccessToken == "" {
		t.Fatalf("expected access token, got %+v", tokenPair)
	}
	if !repo.refreshTokens[hashString].Revoked {
		t.Fatal("expected old refresh token to be revoked")
	}
}

func TestRefreshTokens_RotatedTokenRejected(t *testing.T) {
	repo := newAuthRepo()
	verifier := &authVerifier{claims: &domain.OAuthIdentity{
		Subject: "s", Email: "test@example.com", Name: "T", Provider: "google",
	}}
	uc := newTestAuth(repo, &fakeRevoker{}, verifier, &authPublisher{})

	first, err := uc.AuthenticateGoogle(context.Background(), "id-token")
	if err != nil {
		t.Fatalf("AuthenticateGoogle returned error: %v", err)
	}
	t1 := first.RefreshToken

	second, err := uc.RefreshTokens(context.Background(), t1)
	if err != nil {
		t.Fatalf("first refresh returned error: %v", err)
	}
	if second.RefreshToken == t1 {
		t.Fatal("expected a new refresh token after rotation")
	}

	if _, err := uc.RefreshTokens(context.Background(), t1); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("expected ErrInvalidRefresh on reuse, got %v", err)
	}
}

func TestLogoutRevokesRefreshTokens(t *testing.T) {
	repo := newAuthRepo()
	repo.refreshTokens["h"] = &domain.RefreshToken{ID: "token-1", UserID: "user-1", TokenHash: "h"}
	uc := newTestAuth(repo, &fakeRevoker{}, &authVerifier{}, &authPublisher{})

	if err := uc.Logout(context.Background(), "user-1", "jti-1", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("Logout returned error: %v", err)
	}
	if !repo.refreshTokens["h"].Revoked {
		t.Fatal("expected refresh token revoked")
	}
}

func TestLogout_RevokesAccessJTIWithRemainingTTL(t *testing.T) {
	revoker := &fakeRevoker{}
	uc := newTestAuth(newAuthRepo(), revoker, &authVerifier{}, &authPublisher{})
	exp := time.Now().Add(10 * time.Minute)

	if err := uc.Logout(context.Background(), "user-1", "jti-1", exp); err != nil {
		t.Fatalf("Logout returned error: %v", err)
	}
	if revoker.jti != "jti-1" {
		t.Fatalf("expected jti-1 revoked, got %q", revoker.jti)
	}
	diff := revoker.ttl - time.Until(exp)
	if diff < -time.Second || diff > time.Second {
		t.Fatalf("ttl %v not within 1s of remaining %v", revoker.ttl, time.Until(exp))
	}
}

func TestLogout_RevokerErrorStillSucceeds(t *testing.T) {
	revoker := &fakeRevoker{err: errors.New("redis down")}
	uc := newTestAuth(newAuthRepo(), revoker, &authVerifier{}, &authPublisher{})

	if err := uc.Logout(context.Background(), "user-1", "jti-1", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if revoker.calls != 1 {
		t.Fatalf("expected revoker called once, got %d", revoker.calls)
	}
}
