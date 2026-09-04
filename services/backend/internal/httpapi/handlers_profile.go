package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	_ "image/jpeg"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/admin"
	"github.com/ai-doodoo-slots/services/backend/internal/auth"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// displayNameMin/Max bound casino nicknames.
	displayNameMin = 3
	displayNameMax = 20
	// renameCooldown limits how often a display name can change. The very
	// first rename (never renamed before) is exempt.
	renameCooldown = 24 * time.Hour
	// avatarUploadLimit caps the raw upload body; uploads are decoded,
	// center-cropped, and downscaled to avatarSize before storage.
	avatarUploadLimit = 1 << 20
	avatarSize        = 64
)

// displayNamePattern: letters, digits, spaces, underscores, hyphens.
var displayNamePattern = regexp.MustCompile(`^[A-Za-z0-9 _-]+$`)

var (
	collapseSpaces = regexp.MustCompile(`\s+`)
	trimEdges      = regexp.MustCompile(`^ | $`)
)

// avatarPresetPattern guards the preset sprite key echoed back by clients.
// The curated list itself lives with the web assets.
var avatarPresetPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// validDisplayName trims and checks the nickname rules.
func validDisplayName(s string) (string, bool) {
	s = trimEdges.ReplaceAllString(collapseSpaces.ReplaceAllString(s, " "), "")
	if len(s) < displayNameMin || len(s) > displayNameMax {
		return "", false
	}
	if !displayNamePattern.MatchString(s) {
		return "", false
	}
	return s, true
}

// auditProfile writes an audit_log row for profile changes.
func (s *Server) auditProfile(ctx context.Context, actorID int64, action string, meta map[string]any) {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return
	}
	q := store.New(s.pool)
	_, err = q.InsertAuditEntry(ctx, store.InsertAuditEntryParams{
		ActorUserID: pgtype.Int8{Int64: actorID, Valid: true},
		Action:      action,
		TargetType:  pgtype.Text{String: "user", Valid: true},
		TargetID:    pgtype.Int8{Int64: actorID, Valid: true},
		Metadata:    metaJSON,
	})
	if err != nil {
		s.logger.Warn("profile audit", "err", err)
	}
}

// handleUpdateMe edits the caller's profile: display name and/or avatar
// preset. Both guests and registered players may rename; the Keycloak
// write-back only fires for registered accounts.
func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	var body struct {
		DisplayName  *string `json:"displayName"`
		AvatarPreset *string `json:"avatarPreset"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if body.DisplayName == nil && body.AvatarPreset == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "nothing to update")
		return
	}

	ctx := r.Context()
	q := store.New(s.pool)
	user, err := q.GetUserByID(ctx, su.UserID)
	if err != nil {
		s.logger.Error("profile: user missing", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	if body.DisplayName != nil {
		name, ok := validDisplayName(*body.DisplayName)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_display_name",
				"3-20 characters; letters, digits, spaces, _ and - only")
			return
		}
		if name != user.DisplayName {
			// Cooldown: renames are rate-limited except the first ever.
			if user.DisplayNameUpdatedAt != nil {
				elapsed := s.clock.Now().Sub(*user.DisplayNameUpdatedAt)
				if elapsed < renameCooldown {
					writeJSON(w, http.StatusForbidden, apiError{
						Code:    "rename_cooldown",
						Message: "you can rename again in " + (renameCooldown - elapsed).Truncate(time.Minute).String(),
					})
					return
				}
			}
			taken, err := q.DisplayNameTaken(ctx, store.DisplayNameTakenParams{Lower: name, ID: su.UserID})
			if err != nil {
				s.logger.Error("profile: name check", "err", err)
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
				return
			}
			if taken {
				writeError(w, http.StatusConflict, "name_taken", "that name is already taken")
				return
			}
			user, err = q.UpdateDisplayName(ctx, store.UpdateDisplayNameParams{ID: su.UserID, DisplayName: name})
			if err != nil {
				s.logger.Error("profile: rename", "err", err, "user_id", su.UserID)
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
				return
			}
			s.auditProfile(ctx, su.UserID, "profile.rename", map[string]any{"from": su.DisplayName, "to": name})
		}
	}

	if body.AvatarPreset != nil {
		preset := *body.AvatarPreset
		if preset != "" && !avatarPresetPattern.MatchString(preset) {
			writeError(w, http.StatusBadRequest, "invalid_avatar_preset", "unknown avatar preset")
			return
		}
		if preset == "" {
			if _, err := q.ClearAvatar(ctx, su.UserID); err != nil {
				s.logger.Error("profile: clear avatar", "err", err, "user_id", su.UserID)
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
				return
			}
		} else if _, err := q.SetAvatarPreset(ctx, store.SetAvatarPresetParams{ID: su.UserID, AvatarPreset: pgtype.Text{String: preset, Valid: true}}); err != nil {
			s.logger.Error("profile: set preset", "err", err, "user_id", su.UserID)
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		s.auditProfile(ctx, su.UserID, "profile.avatar", map[string]any{"preset": preset})
	}

	// Re-read so the response and events reflect every applied change.
	user, err = q.GetUserByID(ctx, su.UserID)
	if err != nil {
		s.logger.Error("profile: reread", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	su2 := auth.SessionUserFromStore(&user)
	su2.SessionID = su.SessionID
	su2.Subject = su.Subject

	s.publishProfileEvent(su.UserID, su2.DisplayName, su2.AvatarPreset, su2.AvatarVersion)
	s.kcAdmin.PushProfileAsync(su.Subject, su2.DisplayName, su2.AvatarPreset, su2.AvatarVersion)
	s.writeMe(w, r, su2)
}

// handlePutAvatar stores an uploaded avatar. Uploads are a registered-player
// perk: guests pick presets only. The image is center-cropped, downscaled to
// 64x64 pixels (the house pixel-art look), and stored in Postgres.
func (s *Server) handlePutAvatar(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	if su.IsGuest {
		writeError(w, http.StatusForbidden, "guests_cannot_upload", "log in to upload an avatar")
		return
	}
	ct := r.Header.Get("Content-Type")
	if ct != "image/png" && ct != "image/jpeg" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media", "avatar must be PNG or JPEG")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, avatarUploadLimit))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "avatar must be 1MB or less")
		return
	}
	src, err := decodeAvatar(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_image", "could not decode image")
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, scaleSquare(src, avatarSize)); err != nil {
		s.logger.Error("avatar encode", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	sum := sha256.Sum256(buf.Bytes())

	ctx := r.Context()
	q := store.New(s.pool)
	if err := q.UpsertUserAvatar(ctx, store.UpsertUserAvatarParams{
		UserID:      su.UserID,
		ContentType: "image/png",
		Bytes:       buf.Bytes(),
		Sha256:      hex.EncodeToString(sum[:]),
	}); err != nil {
		s.logger.Error("avatar upsert", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	version, err := q.SetUploadedAvatar(ctx, su.UserID)
	if err != nil {
		s.logger.Error("avatar version bump", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	user, err := q.GetUserByID(ctx, su.UserID)
	if err != nil {
		s.logger.Error("avatar reread", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	s.auditProfile(ctx, su.UserID, "profile.avatar", map[string]any{"upload_sha256": hex.EncodeToString(sum[:])})
	s.publishProfileEvent(su.UserID, user.DisplayName, user.AvatarPreset.String, user.AvatarVersion)
	s.kcAdmin.PushProfileAsync(su.Subject, user.DisplayName, user.AvatarPreset.String, user.AvatarVersion)
	writeJSON(w, http.StatusOK, map[string]any{"avatarVersion": version})
}

// decodeAvatar rejects decompression-bomb dimensions before a full decode.
func decodeAvatar(raw []byte) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 4096 || cfg.Height > 4096 {
		return nil, errors.New("image dimensions out of range")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	return img, err
}

// scaleSquare center-crops to a square and nearest-neighbor scales to size,
// on purpose: the casino renders everything as chunky pixels.
func scaleSquare(src image.Image, size int) *image.RGBA {
	b := src.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	cx := b.Min.X + (b.Dx()-side)/2
	cy := b.Min.Y + (b.Dy()-side)/2
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		sy := cy + y*side/size
		for x := 0; x < size; x++ {
			sx := cx + x*side/size
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

// handleDeleteAvatar clears the avatar entirely (preset and upload).
func (s *Server) handleDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	ctx := r.Context()
	q := store.New(s.pool)
	if err := q.DeleteUserAvatar(ctx, su.UserID); err != nil {
		s.logger.Error("avatar delete", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	if _, err := q.ClearAvatar(ctx, su.UserID); err != nil {
		s.logger.Error("avatar clear", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	s.publishProfileEvent(su.UserID, su.DisplayName, "", su.AvatarVersion+1)
	s.kcAdmin.PushProfileAsync(su.Subject, su.DisplayName, "", su.AvatarVersion+1)
	w.WriteHeader(http.StatusNoContent)
}

// handleUserAvatar serves a stored avatar upload. Clients only request this
// URL when the profile has no preset and a non-zero version, so the version
// query makes the response effectively immutable.
func (s *Server) handleUserAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user id")
		return
	}
	row, err := store.New(s.pool).GetUserAvatar(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "avatar_none", "no uploaded avatar")
			return
		}
		s.logger.Error("avatar fetch", "err", err, "user_id", id)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	w.Header().Set("Content-Type", row.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(row.Bytes)
}

// handleUserPublicProfile serves the public slice of a player's profile for
// profile cards and game surfaces.
func (s *Server) handleUserPublicProfile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user id")
		return
	}
	row, err := store.New(s.pool).GetUserPublicProfile(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "unknown_user", "no such player")
			return
		}
		s.logger.Error("public profile", "err", err, "user_id", id)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":            row.ID,
		"displayName":   row.DisplayName,
		"avatarPreset":  row.AvatarPreset.String,
		"avatarVersion": row.AvatarVersion,
		"role":          row.Role,
		"createdAt":     row.CreatedAt,
	})
}

// handleAdminListUsers lists users for the staff panel (moderator+), with an
// optional display-name/email substring search.
func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	if s.requireRole(w, r, admin.RoleModerator) == nil {
		return
	}
	limit := int32(50)
	if lv := r.URL.Query().Get("limit"); lv != "" {
		v, err := strconv.ParseInt(lv, 10, 32)
		if err != nil || v < 1 || v > 100 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be 1-100")
			return
		}
		limit = int32(v)
	}
	rows, err := store.New(s.pool).AdminListUsers(r.Context(), store.AdminListUsersParams{
		Term:    r.URL.Query().Get("query"),
		MaxRows: limit,
	})
	if err != nil {
		s.logger.Error("admin user list", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	type userRowDTO struct {
		ID            int64      `json:"id"`
		DisplayName   string     `json:"displayName"`
		IsGuest       bool       `json:"isGuest"`
		Email         *string    `json:"email"`
		EmailVerified bool       `json:"emailVerified"`
		Role          string     `json:"role"`
		Status        string     `json:"status"`
		StatusUntil   *time.Time `json:"statusUntil"`
		CreatedAt     time.Time  `json:"createdAt"`
	}
	out := make([]userRowDTO, 0, len(rows))
	for _, u := range rows {
		out = append(out, userRowDTO{
			ID:            u.ID,
			DisplayName:   u.DisplayName,
			IsGuest:       u.IsGuest,
			Email:         u.Email,
			EmailVerified: u.EmailVerifiedAt != nil,
			Role:          u.Role,
			Status:        u.Status,
			StatusUntil:   u.StatusUntil,
			CreatedAt:     u.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}
