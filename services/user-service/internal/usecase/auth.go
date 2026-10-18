package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/piresc/go-project/services/user-service/internal/domain"
)

// AuthUseCase owns authentication, refresh, and logout flows.
type AuthUseCase struct {
	repo       UserRepository
	cache      UserCache
	revoker    TokenRevoker
	publisher  EventPublisher
	googleAuth OAuthVerifier
	appleAuth  OAuthVerifier
	tokenSvc   TokenService
	accessTTL  time.Duration
	log        *slog.Logger
}

// NewAuthUseCase constructs the auth use case.
func NewAuthUseCase(repo UserRepository, cache UserCache, revoker TokenRevoker, publisher EventPublisher, googleAuth, appleAuth OAuthVerifier, tokenSvc TokenService, accessTTL time.Duration, log *slog.Logger) *AuthUseCase {
	return &AuthUseCase{
		repo:       repo,
		cache:      cache,
		revoker:    revoker,
		publisher:  publisher,
		googleAuth: googleAuth,
		appleAuth:  appleAuth,
		tokenSvc:   tokenSvc,
		accessTTL:  accessTTL,
		log:        log,
	}
}

// AuthenticateGoogle authenticates a Google ID token and returns a token pair.
func (uc *AuthUseCase) AuthenticateGoogle(ctx context.Context, idToken string) (*domain.TokenPair, error) {
	return uc.authenticate(ctx, uc.googleAuth, idToken)
}

// AuthenticateApple authenticates an Apple ID token and returns a token pair.
func (uc *AuthUseCase) AuthenticateApple(ctx context.Context, idToken string) (*domain.TokenPair, error) {
	return uc.authenticate(ctx, uc.appleAuth, idToken)
}

func (uc *AuthUseCase) authenticate(ctx context.Context, verifier OAuthVerifier, idToken string) (*domain.TokenPair, error) {
	claims, err := verifier.Verify(ctx, idToken)
	if err != nil {
		return nil, err
	}

	user, isNew, err := uc.findOrCreateUser(ctx, claims)
	if err != nil {
		return nil, err
	}
	if isNew {
		uc.publishUserCreated(ctx, user)
	}

	return uc.issueTokenPair(ctx, user)
}

// RefreshTokens rotates a refresh token and returns a new token pair.
func (uc *AuthUseCase) RefreshTokens(ctx context.Context, refreshToken string) (*domain.TokenPair, error) {
	hash := sha256.Sum256([]byte(refreshToken))
	hashString := hex.EncodeToString(hash[:])

	storedToken, err := uc.repo.FindRefreshTokenByHash(ctx, hashString)
	if err != nil {
		return nil, err
	}
	if storedToken == nil || storedToken.Revoked {
		return nil, ErrInvalidRefresh
	}
	if time.Now().After(storedToken.ExpiresAt) {
		return nil, ErrRefreshExpired
	}

	if err := uc.repo.RevokeRefreshToken(ctx, storedToken.ID); err != nil {
		return nil, err
	}

	user, err := uc.repo.FindByID(ctx, storedToken.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidRefresh
	}

	return uc.issueTokenPair(ctx, user)
}

// Logout revokes refresh tokens and marks the access token JTI as revoked.
func (uc *AuthUseCase) Logout(ctx context.Context, userID, accessTokenJTI string, tokenExp time.Time) error {
	if err := uc.repo.RevokeAllUserRefreshTokens(ctx, userID); err != nil {
		return err
	}

	remaining := time.Until(tokenExp)
	if remaining > 0 {
		if err := uc.revoker.Revoke(ctx, accessTokenJTI, remaining); err != nil {
			uc.log.WarnContext(ctx, "failed to add JTI to revocation list", "user_id", userID, "error", err)
		}
	}
	return nil
}

func (uc *AuthUseCase) findOrCreateUser(ctx context.Context, claims *domain.OAuthIdentity) (*domain.User, bool, error) {
	// When email is available, run FindByProvider and FindByEmail concurrently
	// to reduce cumulative DB latency on the new/link-user path.
	if claims.Email != "" {
		var providerUser, emailUser *domain.User

		g, gctx := errgroup.WithContext(ctx)

		g.Go(func() error {
			u, err := uc.repo.FindByProvider(gctx, claims.Provider, claims.Subject)
			if err != nil {
				return err
			}
			providerUser = u
			return nil
		})

		g.Go(func() error {
			u, err := uc.repo.FindByEmail(gctx, claims.Email)
			if err != nil {
				return err
			}
			emailUser = u
			return nil
		})

		if err := g.Wait(); err != nil {
			return nil, false, err
		}

		// Provider match takes priority: user already linked.
		if providerUser != nil {
			return providerUser, false, nil
		}

		// Email match: link the new provider to the existing user.
		if emailUser != nil {
			authProvider := &domain.AuthProvider{
				UserID:          emailUser.ID,
				Provider:        claims.Provider,
				ProviderSubject: claims.Subject,
			}
			if err := uc.repo.CreateAuthProvider(ctx, authProvider); err != nil {
				return nil, false, err
			}
			return emailUser, false, nil
		}
	} else {
		// No email: only look up by provider.
		user, err := uc.repo.FindByProvider(ctx, claims.Provider, claims.Subject)
		if err != nil {
			return nil, false, err
		}
		if user != nil {
			return user, false, nil
		}
	}

	// Neither lookup found a user: create a new one.
	displayName := claims.Name
	if displayName == "" {
		displayName = claims.Email
	}

	user := &domain.User{
		Email:       claims.Email,
		DisplayName: displayName,
		Role:        "investor",
	}
	if err := uc.repo.Create(ctx, user); err != nil {
		return nil, false, err
	}

	authProvider := &domain.AuthProvider{
		UserID:          user.ID,
		Provider:        claims.Provider,
		ProviderSubject: claims.Subject,
	}
	if err := uc.repo.CreateAuthProvider(ctx, authProvider); err != nil {
		return nil, false, err
	}

	return user, true, nil
}

func (uc *AuthUseCase) issueTokenPair(ctx context.Context, user *domain.User) (*domain.TokenPair, error) {
	accessToken, _, _, err := uc.tokenSvc.GenerateAccessToken(user)
	if err != nil {
		return nil, err
	}

	plaintext, hash, exp, err := uc.tokenSvc.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}

	refreshToken := &domain.RefreshToken{
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: exp,
		Revoked:   false,
	}
	if err := uc.repo.CreateRefreshToken(ctx, refreshToken); err != nil {
		return nil, err
	}

	return &domain.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: plaintext,
		ExpiresIn:    int(uc.accessTTL.Seconds()),
	}, nil
}

func (uc *AuthUseCase) publishUserCreated(ctx context.Context, user *domain.User) {
	event := domain.UserCreatedEvent{
		UserID: user.ID,
		Email:  user.Email,
		Role:   user.Role,
	}
	if err := uc.publisher.PublishUserCreated(ctx, event); err != nil {
		uc.log.WarnContext(ctx, "failed to publish user.created event", "user_id", user.ID, "error", err)
	}
}
