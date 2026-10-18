package httptransport

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/piresc/go-project/services/user-service/internal/domain"
)

type authRequest struct {
	IDToken string `json:"id_token"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type updateProfileRequest struct {
	DisplayName *string `json:"display_name"`
}

// userProfileResponse is the DTO returned by profile endpoints. It adds a
// human-friendly member_since (YYYY-MM-DD) next to RFC 3339 timestamps.
type userProfileResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Role        string `json:"role"`
	MemberSince string `json:"member_since"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func toProfileResponse(user *domain.User) userProfileResponse {
	return userProfileResponse{
		ID:          user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		AvatarURL:   user.AvatarURL,
		Role:        user.Role,
		MemberSince: user.CreatedAt.Format("2006-01-02"),
		CreatedAt:   user.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   user.UpdatedAt.Format(time.RFC3339),
	}
}

// decodeJSON strictly decodes the request body into dst, rejecting unknown fields.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
