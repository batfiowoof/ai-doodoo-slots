package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
)

// AdminClient writes profile projections (display name, avatar attributes)
// back to Keycloak via the Admin REST API, authenticating with the
// retro-api service account. The local database remains the runtime
// authority — Keycloak attributes exist so tokens and future OIDC consumers
// see coherent profile data. A nil client means write-back is disabled and
// every call is a no-op.
type AdminClient struct {
	realm    string
	adminURL string
	clientID string
	secret   string
	tokenURL string
	clk      clock.Clock
	log      *slog.Logger
	http     *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// NewAdminClient wires the write-back client. Any missing piece disables it
// (returns nil), matching the optional-OIDC pattern of the verifier.
func NewAdminClient(issuer, adminURL, clientID, secret string, clk clock.Clock, log *slog.Logger) *AdminClient {
	issuer = strings.TrimSuffix(issuer, "/")
	adminURL = strings.TrimSuffix(adminURL, "/")
	if issuer == "" || adminURL == "" || clientID == "" || secret == "" {
		return nil
	}
	const realmsSegment = "/realms/"
	i := strings.Index(issuer, realmsSegment)
	if i < 0 {
		return nil
	}
	realm := issuer[i+len(realmsSegment):]
	return &AdminClient{
		realm:    realm,
		adminURL: adminURL,
		clientID: clientID,
		secret:   secret,
		tokenURL: issuer + "/protocol/openid-connect/token",
		clk:      clk,
		log:      log,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

// accessToken returns a cached client_credentials token, refreshing it a
// little before expiry.
func (a *AdminClient) accessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && a.clk.Now().Before(a.tokenExp) {
		return a.token, nil
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {a.clientID},
		"client_secret": {a.secret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint %d: %s", resp.StatusCode, truncate(body))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tr); err != nil || tr.AccessToken == "" {
		return "", fmt.Errorf("token endpoint response malformed")
	}
	a.token = tr.AccessToken
	a.tokenExp = a.clk.Now().Add(time.Duration(tr.ExpiresIn)*time.Second - 30*time.Second)
	return a.token, nil
}

// UpdateProfile merges the profile projection into the Keycloak user's
// attributes. It reads the current representation first because the Admin
// API's PUT replaces attributes wholesale. Caller decides sync vs async.
func (a *AdminClient) UpdateProfile(ctx context.Context, subject, displayName string, avatarPreset string, avatarVersion int64) error {
	tok, err := a.accessToken(ctx)
	if err != nil {
		return err
	}
	userURL := fmt.Sprintf("%s/admin/realms/%s/users/%s", a.adminURL, a.realm, subject)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get user %d: %s", resp.StatusCode, truncate(body))
	}
	var rep map[string]any
	if err := json.Unmarshal(body, &rep); err != nil {
		return err
	}
	// "access" is a read-only evaluation block; echoing it back makes the
	// PUT answer 400. Delete before the round-trip.
	delete(rep, "access")

	attrs, _ := rep["attributes"].(map[string]any)
	if attrs == nil {
		attrs = map[string]any{}
	}
	attrs["displayName"] = []string{displayName}
	if avatarPreset != "" {
		attrs["avatarPreset"] = []string{avatarPreset}
		attrs["avatarVersion"] = []string{fmt.Sprintf("%d", avatarVersion)}
	} else if avatarVersion > 0 {
		delete(attrs, "avatarPreset")
		attrs["avatarVersion"] = []string{fmt.Sprintf("%d", avatarVersion)}
	} else {
		delete(attrs, "avatarPreset")
		delete(attrs, "avatarVersion")
	}
	rep["attributes"] = attrs

	putBody, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	tok, err = a.accessToken(ctx)
	if err != nil {
		return err
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPut, userURL, strings.NewReader(string(putBody)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp2, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp2.Body, 1<<20))
	if resp2.StatusCode != http.StatusNoContent {
		return fmt.Errorf("put user %d", resp2.StatusCode)
	}
	return nil
}

// PushProfileAsync is the fire-and-forget wrapper handlers use: profile
// edits must never fail because Keycloak hiccupped. Errors are logged.
func (a *AdminClient) PushProfileAsync(subject, displayName string, avatarPreset string, avatarVersion int64) {
	if a == nil || subject == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := a.UpdateProfile(ctx, subject, displayName, avatarPreset, avatarVersion); err != nil {
			a.log.Warn("keycloak profile write-back failed", "subject", subject, "err", err)
		}
	}()
}

func truncate(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
