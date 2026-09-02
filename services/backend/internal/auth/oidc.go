package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/golang-jwt/jwt/v5"
)

// OIDCConfig configures the Keycloak integration. Issuer is the public URL
// tokens are minted with; the endpoint overrides exist so callers inside a
// container network can reach Keycloak over an internal hostname while
// tokens still carry the public issuer.
type OIDCConfig struct {
	Issuer        string
	ClientID      string
	AuthURL       string // override for discovery-derived endpoint
	TokenURL      string
	JWKSURL       string
	EndSessionURL string
}

// KeycloakClaims is the subset of the access token this service consumes.
type KeycloakClaims struct {
	Subject       string
	PreferredName string
	Email         string
	EmailVerified bool
	RealmRoles    []string
	ExpiresAt     time.Time
}

// RoleFromRealmRoles picks the highest app role present in the token's
// realm_access.roles claim. Unknown roles are ignored; the default is player.
func RoleFromRealmRoles(roles []string) string {
	best := "player"
	rank := map[string]int{"player": 1, "moderator": 2, "admin": 3}
	for _, r := range roles {
		if rank[r] > rank[best] {
			best = r
		}
	}
	return best
}

// ErrUnauthorized covers every invalid-token case with the same
// outward-visible outcome: the request is unauthenticated.
var ErrUnauthorized = errors.New("invalid or expired token")

const jwksCacheTTL = 15 * time.Minute

// OIDCVerifier validates Keycloak access tokens against the realm's JWKS.
// Keys are cached and refreshed on an unknown kid or a TTL expiry.
type OIDCVerifier struct {
	cfg    OIDCConfig
	client *http.Client
	clk    clock.Clock
	logger *slog.Logger

	mu       sync.Mutex
	doc      *discoveryDoc
	keys     map[string]*rsa.PublicKey
	fetched  time.Time
	keysSeen bool
}

// NewOIDCVerifier builds a verifier. Endpoint overrides in cfg replace
// discovery-derived values, enabling split public/internal network layouts.
func NewOIDCVerifier(cfg OIDCConfig, clk clock.Clock, logger *slog.Logger) *OIDCVerifier {
	return &OIDCVerifier{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Second},
		clk:    clk,
		logger: logger,
	}
}

// AuthURL returns the authorization endpoint (browser-facing).
func (v *OIDCVerifier) AuthURL(ctx context.Context) (string, error) {
	if v.cfg.AuthURL != "" {
		return v.cfg.AuthURL, nil
	}
	d, err := v.discover(ctx)
	if err != nil {
		return "", err
	}
	return d.AuthorizationEndpoint, nil
}

// TokenURL returns the token endpoint (server-facing).
func (v *OIDCVerifier) TokenURL(ctx context.Context) (string, error) {
	if v.cfg.TokenURL != "" {
		return v.cfg.TokenURL, nil
	}
	d, err := v.discover(ctx)
	if err != nil {
		return "", err
	}
	return d.TokenEndpoint, nil
}

// EndSessionURL returns the RP-initiated logout endpoint (browser-facing).
func (v *OIDCVerifier) EndSessionURL(ctx context.Context) (string, error) {
	if v.cfg.EndSessionURL != "" {
		return v.cfg.EndSessionURL, nil
	}
	d, err := v.discover(ctx)
	if err != nil {
		return "", err
	}
	return d.EndSessionEndpoint, nil
}

type discoveryDoc struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
}

// discover resolves endpoints. Only used when no overrides are configured.
func (v *OIDCVerifier) discover(ctx context.Context) (*discoveryDoc, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.doc != nil {
		return v.doc, nil
	}
	url := v.cfg.Issuer + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("discovery request: %w", err)
	}
	res, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discovery fetch: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery status %d", res.StatusCode)
	}
	var doc discoveryDoc
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("discovery decode: %w", err)
	}
	if doc.Issuer != v.cfg.Issuer {
		return nil, fmt.Errorf("issuer mismatch: config %q, document %q", v.cfg.Issuer, doc.Issuer)
	}
	v.doc = &doc
	return &doc, nil
}

// Verify validates a raw access token and returns its claims.
func (v *OIDCVerifier) Verify(ctx context.Context, raw string) (*KeycloakClaims, error) {
	key, err := v.signingKey(ctx, raw)
	if err != nil {
		return nil, err
	}
	tok, err := jwt.Parse(raw, func(*jwt.Token) (any, error) {
		return key, nil
	},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.cfg.Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil || !tok.Valid {
		return nil, ErrUnauthorized
	}
	mc, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrUnauthorized
	}
	// Audience check with the OIDC authorized-party fallback: Keycloak's
	// default mappers set aud to "account" while azp names the intended
	// client, so azp == clientID is a valid match.
	if v.cfg.ClientID != "" && !audContains(mc, v.cfg.ClientID) && !azpMatches(mc, v.cfg.ClientID) {
		return nil, ErrUnauthorized
	}
	return claimsFromMap(mc), nil
}

func audContains(mc jwt.MapClaims, clientID string) bool {
	switch aud := mc["aud"].(type) {
	case string:
		return aud == clientID
	case []any:
		for _, a := range aud {
			if s, ok := a.(string); ok && s == clientID {
				return true
			}
		}
	}
	return false
}

func azpMatches(mc jwt.MapClaims, clientID string) bool {
	azp, _ := mc["azp"].(string)
	return azp == clientID
}

func claimsFromMap(mc jwt.MapClaims) *KeycloakClaims {
	c := &KeycloakClaims{}
	c.Subject, _ = mc["sub"].(string)
	c.PreferredName, _ = mc["preferred_username"].(string)
	c.Email, _ = mc["email"].(string)
	if ev, ok := mc["email_verified"].(bool); ok {
		c.EmailVerified = ev
	}
	if exp, err := mc.GetExpirationTime(); err == nil && exp != nil {
		c.ExpiresAt = time.Unix(exp.Unix(), 0).UTC()
	}
	if ra, ok := mc["realm_access"].(map[string]any); ok {
		if roles, ok := ra["roles"].([]any); ok {
			for _, r := range roles {
				if s, ok := r.(string); ok {
					c.RealmRoles = append(c.RealmRoles, s)
				}
			}
		}
	}
	return c
}

// signingKey resolves the RSA public key for a token's kid, refreshing the
// JWKS cache when the kid is unknown or the cache is stale. An unverified
// kid can only steer key selection, never validation itself.
func (v *OIDCVerifier) signingKey(ctx context.Context, raw string) (any, error) {
	kid := kidOf(raw)
	if kid == "" {
		return nil, ErrUnauthorized
	}

	v.mu.Lock()
	if v.keysSeen && v.clk.Now().Sub(v.fetched) < jwksCacheTTL {
		key, ok := v.keys[kid]
		v.mu.Unlock()
		if ok {
			return key, nil
		}
		return nil, ErrUnauthorized
	}
	v.mu.Unlock()

	keys, err := v.fetchKeys(ctx)
	if err != nil {
		v.logger.Error("jwks fetch", "err", err)
		return nil, ErrUnauthorized
	}

	v.mu.Lock()
	v.keys = keys
	v.fetched = v.clk.Now()
	v.keysSeen = true
	key, ok := keys[kid]
	v.mu.Unlock()
	if !ok {
		return nil, ErrUnauthorized
	}
	return key, nil
}

type jwksDoc struct {
	Keys []jwksKey `json:"keys"`
}

type jwksKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (v *OIDCVerifier) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	jwksURI := v.cfg.JWKSURL
	if jwksURI == "" {
		d, err := v.discover(ctx)
		if err != nil {
			return nil, err
		}
		jwksURI = d.JWKSURI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, err
	}
	res, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks status %d", res.StatusCode)
	}
	var doc jwksDoc
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, err
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pub, err := rsaFromJWK(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("jwks contained no RSA keys")
	}
	return keys, nil
}

func rsaFromJWK(nB64, eB64 string) (*rsa.PublicKey, error) {
	n, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	e, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	eInt := 0
	for _, b := range e {
		eInt = eInt<<8 | int(b)
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: eInt}, nil
}

// kidOf extracts the kid header without verifying the token. Verification
// happens afterwards with the resolved key.
func kidOf(raw string) string {
	parts := splitJWS(raw)
	if parts == nil {
		return ""
	}
	var hdr struct {
		Kid string `json:"kid"`
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ""
	}
	if err := json.Unmarshal(b, &hdr); err != nil {
		return ""
	}
	return hdr.Kid
}

func splitJWS(raw string) []string {
	var out []string
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == '.' {
			out = append(out, raw[start:i])
			start = i + 1
		}
	}
	out = append(out, raw[start:])
	if len(out) != 3 {
		return nil
	}
	return out
}
