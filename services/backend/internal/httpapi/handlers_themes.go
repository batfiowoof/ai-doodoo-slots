package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/theme"
)

type spriteDTO struct {
	Name string   `json:"name"`
	Rows []string `json:"rows"`
}

type themeDTO struct {
	ID        int64       `json:"id"`
	Name      string      `json:"name"`
	Palette   []string    `json:"palette"`
	Sprites   []spriteDTO `json:"sprites"`
	CreatedAt time.Time   `json:"createdAt"`
}

func toThemeDTO(t theme.StoredTheme) (themeDTO, error) {
	var palette []string
	if err := json.Unmarshal(t.Palette, &palette); err != nil {
		return themeDTO{}, err
	}
	var sprites []spriteDTO
	if err := json.Unmarshal(t.Sprites, &sprites); err != nil {
		return themeDTO{}, err
	}
	return themeDTO{
		ID:        t.ID,
		Name:      t.Name,
		Palette:   palette,
		Sprites:   sprites,
		CreatedAt: t.CreatedAt,
	}, nil
}

// handleCreateTheme generates (or returns a stored) theme for the caller.
func (s *Server) handleCreateTheme(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	if s.themes == nil {
		writeError(w, http.StatusServiceUnavailable, "themes_unavailable", "theme generation is not configured")
		return
	}

	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	created, err := s.themes.Generate(r.Context(), su.UserID, body.Prompt)
	switch {
	case errors.Is(err, theme.ErrPromptInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	case errors.Is(err, theme.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "rate_limited", err.Error())
		return
	case errors.Is(err, theme.ErrNoAPIKey):
		writeError(w, http.StatusServiceUnavailable, "themes_unavailable", "theme generation is not configured")
		return
	case errors.Is(err, theme.ErrProviderFailed):
		s.logger.Warn("theme generation failed", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusBadGateway, "theme_failed", "theme generation failed; your machine keeps its current theme")
		return
	case err != nil:
		s.logger.Error("create theme", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	dto, err := toThemeDTO(created)
	if err != nil {
		s.logger.Error("decode stored theme", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, dto)
}

// handleListThemes returns the user's saved themes.
func (s *Server) handleListThemes(w http.ResponseWriter, r *http.Request) {
	su := s.currentUser(r)
	if su == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "no active session")
		return
	}
	if s.themes == nil {
		writeJSON(w, http.StatusOK, []themeDTO{})
		return
	}
	rows, err := s.themes.List(r.Context(), su.UserID)
	if err != nil {
		s.logger.Error("list themes", "err", err, "user_id", su.UserID)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	themes := make([]themeDTO, 0, len(rows))
	for _, t := range rows {
		dto, err := toThemeDTO(t)
		if err != nil {
			s.logger.Error("decode stored theme", "err", err)
			continue
		}
		themes = append(themes, dto)
	}
	writeJSON(w, http.StatusOK, themes)
}
