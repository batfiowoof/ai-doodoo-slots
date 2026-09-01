// Package theme implements AI-generated pixel-art themes via OpenRouter.
// Every provider response is validated in full before it is stored or
// applied: a malformed response is rejected whole and the machine keeps its
// previous theme. Emoji cannot coexist with pixel art, so the model
// generates actual 16x16 hex-indexed sprites.
package theme

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	SpriteSize   = 16 // 16x16 sprites
	SpriteCount  = 8  // ordered common to rare, matching the symbol tables
	PaletteCount = 8  // index 0 is transparency
	MaxNameLen   = 48
	MaxSpriteNameLen = 24
)

// Sprite is one 16x16 pixel-art sprite: 16 rows of 16 hex characters, each
// character indexing the palette.
type Sprite struct {
	Name string   `json:"name"`
	Rows []string `json:"rows"`
}

// Theme is a validated generated theme.
type Theme struct {
	Name    string   `json:"name"`
	Palette []string `json:"palette"`
	Sprites []Sprite `json:"sprites"`
}

var (
	ErrEmptyName      = errors.New("theme name is empty")
	ErrNameTooLong    = errors.New("theme name too long")
	ErrBadPalette     = errors.New("palette must be exactly 8 hex colors")
	ErrBadSpriteCount = errors.New("theme must have exactly 8 sprites")
	ErrBadSprite      = errors.New("sprite must be 16 rows of 16 hex characters")
)

// parseHexColor accepts #rrggbb and #rrggbbaa.
func parseHexColor(s string) error {
	v := strings.TrimPrefix(s, "#")
	switch len(v) {
	case 6, 8:
	default:
		return fmt.Errorf("color %q must be #rrggbb or #rrggbbaa", s)
	}
	if _, err := hex.DecodeString(v); err != nil {
		return fmt.Errorf("color %q is not valid hex", s)
	}
	return nil
}

// Validate enforces the full contract before anything is stored or applied.
// Any failure rejects the whole theme.
func Validate(t Theme) error {
	if t.Name == "" {
		return ErrEmptyName
	}
	if len(t.Name) > MaxNameLen {
		return ErrNameTooLong
	}
	if len(t.Palette) != PaletteCount {
		return fmt.Errorf("%w: got %d", ErrBadPalette, len(t.Palette))
	}
	for i, c := range t.Palette {
		if err := parseHexColor(c); err != nil {
			return fmt.Errorf("palette[%d]: %w", i, err)
		}
	}
	if len(t.Sprites) != SpriteCount {
		return fmt.Errorf("%w: got %d", ErrBadSpriteCount, len(t.Sprites))
	}
	for i, s := range t.Sprites {
		if s.Name == "" || len(s.Name) > MaxSpriteNameLen {
			return fmt.Errorf("sprite[%d] name must be 1-%d chars", i, MaxSpriteNameLen)
		}
		if len(s.Rows) != SpriteSize {
			return fmt.Errorf("sprite[%d] %q: %w: got %d rows", i, s.Name, ErrBadSprite, len(s.Rows))
		}
		for y, row := range s.Rows {
			if len(row) != SpriteSize {
				return fmt.Errorf("sprite[%d] %q row %d: %w: got %d chars", i, s.Name, y, ErrBadSprite, len(row))
			}
			for x := 0; x < SpriteSize; x++ {
				ch := row[x]
				if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
					return fmt.Errorf("sprite[%d] %q row %d col %d: %q is not a hex digit: %w", i, s.Name, y, x, ch, ErrBadSprite)
				}
			}
		}
	}
	return nil
}

// NormalizeSprite lowercases row characters so downstream rendering is
// deterministic.
func NormalizeSprite(s Sprite) Sprite {
	rows := make([]string, len(s.Rows))
	for i, r := range s.Rows {
		rows[i] = strings.ToLower(r)
	}
	return Sprite{Name: s.Name, Rows: rows}
}

// Normalize returns a validated theme with lowercased sprite rows.
func Normalize(t Theme) Theme {
	sprites := make([]Sprite, len(t.Sprites))
	for i, s := range t.Sprites {
		sprites[i] = NormalizeSprite(s)
	}
	return Theme{Name: t.Name, Palette: t.Palette, Sprites: sprites}
}
