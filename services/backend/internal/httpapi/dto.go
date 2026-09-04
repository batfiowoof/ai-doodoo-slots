package httpapi

import (
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/auth"
)

type userDTO struct {
	ID            int64     `json:"id"`
	DisplayName   string    `json:"displayName"`
	IsGuest       bool      `json:"isGuest"`
	Email         *string   `json:"email"`
	EmailVerified bool      `json:"emailVerified"`
	Role          string    `json:"role"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
	// AvatarPreset names a curated sprite ("" = none); AvatarVersion
	// cache-busts /users/{id}/avatar and signals an uploaded image when > 0.
	AvatarPreset  string `json:"avatarPreset"`
	AvatarVersion int64  `json:"avatarVersion"`
}

func toUserDTO(u *auth.SessionUser) userDTO {
	return userDTO{
		ID:            u.UserID,
		DisplayName:   u.DisplayName,
		IsGuest:       u.IsGuest,
		Email:         u.Email,
		EmailVerified: u.EmailVerifiedAt != nil,
		Role:          u.Role,
		Status:        u.Status,
		CreatedAt:     u.CreatedAt,
		AvatarPreset:  u.AvatarPreset,
		AvatarVersion: u.AvatarVersion,
	}
}

type meDTO struct {
	User           userDTO `json:"user"`
	BalanceCredits int64   `json:"balanceCredits"`
}
