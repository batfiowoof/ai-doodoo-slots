package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/golang-jwt/jwt/v5"
)

// testNow is a fixed base time in the near future; time.Now is banned
// outside the clock package, and jwt validation compares exp to wall time.
var testNow = time.Unix(1893456000, 0).UTC() // 2030-01-01

// testRealm spins an in-process JWKS endpoint backed by a generated key so
// token validation is exercised end to end without Keycloak.
type testRealm struct {
	t   *testing.T
	key *rsa.PrivateKey
	srv *httptest.Server
}

func newTestRealm(t *testing.T) *testRealm {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	r := &testRealm{t: t, key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kid": "test-key",
				"kty": "RSA",
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// sign mints a token with the given claims.
func (r *testRealm) sign(claims jwt.MapClaims) string {
	r.t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "test-key"
	s, err := tok.SignedString(r.key)
	if err != nil {
		r.t.Fatalf("sign token: %v", err)
	}
	return s
}

func (r *testRealm) claims(sub string, issuedAt, expires time.Time, roles []string) jwt.MapClaims {
	c := jwt.MapClaims{
		"iss": "https://auth.example.test/realms/retro-casino",
		"sub": sub,
		"aud": "web",
		"iat": issuedAt.Unix(),
		"exp": expires.Unix(),
		"preferred_username": sub,
	}
	if roles != nil {
		c["realm_access"] = map[string]any{"roles": roles}
	}
	return c
}

func newTestVerifier(t *testing.T, r *testRealm) *OIDCVerifier {
	t.Helper()
	return NewOIDCVerifier(OIDCConfig{
		Issuer:   "https://auth.example.test/realms/retro-casino",
		ClientID: "web",
		JWKSURL:  r.srv.URL + "/jwks",
	}, clock.Real{}, testLogger())
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestVerifyAcceptsValidToken(t *testing.T) {
	r := newTestRealm(t)
	v := newTestVerifier(t, r)
	now := testNow

	claims, err := v.Verify(context.Background(), r.sign(r.claims("user-1", now.Add(-time.Minute), now.Add(time.Hour), []string{"moderator"})))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Errorf("sub = %q", claims.Subject)
	}
	if got := RoleFromRealmRoles(claims.RealmRoles); got != "moderator" {
		t.Errorf("role = %q, want moderator", got)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	r := newTestRealm(t)
	v := newTestVerifier(t, r)

	// Fixed timestamps safely in the real past; jwt validates against the
	// wall clock and time.Now is banned here.
	_, err := v.Verify(context.Background(), r.sign(r.claims("user-1",
		time.Unix(1500000000, 0), time.Unix(1600000000, 0), nil)))
	if err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	r := newTestRealm(t)
	other := newTestRealm(t)
	v := newTestVerifier(t, r)
	now := testNow

	_, err := v.Verify(context.Background(), other.sign(other.claims("user-1", now.Add(-time.Minute), now.Add(time.Hour), nil)))
	if err == nil {
		t.Fatal("expected foreign-key signature to be rejected")
	}
}

func TestVerifyRejectsWrongIssuer(t *testing.T) {
	r := newTestRealm(t)
	v := newTestVerifier(t, r)
	now := testNow

	c := r.claims("user-1", now.Add(-time.Minute), now.Add(time.Hour), nil)
	c["iss"] = "https://evil.example.test/realms/retro-casino"
	if _, err := v.Verify(context.Background(), r.sign(c)); err == nil {
		t.Fatal("expected wrong issuer to be rejected")
	}
}

func TestVerifyRejectsWrongAudience(t *testing.T) {
	r := newTestRealm(t)
	v := newTestVerifier(t, r)
	now := testNow

	c := r.claims("user-1", now.Add(-time.Minute), now.Add(time.Hour), nil)
	c["aud"] = "someone-else"
	if _, err := v.Verify(context.Background(), r.sign(c)); err == nil {
		t.Fatal("expected wrong audience to be rejected")
	}
}

func TestVerifyRejectsAlgDowngrade(t *testing.T) {
	r := newTestRealm(t)
	v := newTestVerifier(t, r)
	now := testNow

	tok := jwt.NewWithClaims(jwt.SigningMethodNone, r.claims("user-1", now.Add(-time.Minute), now.Add(time.Hour), nil))
	tok.Header["kid"] = "test-key"
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}
	if _, err := v.Verify(context.Background(), signed); err == nil {
		t.Fatal("expected alg=none token to be rejected")
	}
}

func TestVerifyAcceptsAzpWhenAudIsAccount(t *testing.T) {
	r := newTestRealm(t)
	v := newTestVerifier(t, r)
	now := testNow

	// Keycloak's default mappers set aud to "account"; the authorized
	// party (azp) is what identifies the intended client.
	c := r.claims("user-1", now.Add(-time.Minute), now.Add(time.Hour), nil)
	c["aud"] = "account"
	c["azp"] = "web"
	if _, err := v.Verify(context.Background(), r.sign(c)); err != nil {
		t.Fatalf("azp fallback should accept: %v", err)
	}

	// A foreign azp must still be rejected.
	c["azp"] = "someone-else"
	if _, err := v.Verify(context.Background(), r.sign(c)); err == nil {
		t.Fatal("expected foreign azp to be rejected")
	}
}

func TestRoleFromRealmRolesPicksHighest(t *testing.T) {
	cases := []struct {
		roles []string
		want  string
	}{
		{nil, "player"},
		{[]string{"offline_access", "default-roles-x"}, "player"},
		{[]string{"player", "moderator"}, "moderator"},
		{[]string{"moderator", "admin", "player"}, "admin"},
	}
	for _, tc := range cases {
		if got := RoleFromRealmRoles(tc.roles); got != tc.want {
			t.Errorf("RoleFromRealmRoles(%v) = %q, want %q", tc.roles, got, tc.want)
		}
	}
}
