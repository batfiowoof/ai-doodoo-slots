package auth

import (
	"strings"
	"testing"
)

func TestHashPasswordRoundtrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=") {
		t.Fatalf("hash not PHC argon2id: %q", hash)
	}
	ok, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("correct password rejected")
	}
	ok, err = VerifyPassword("wrong password", hash)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("wrong password accepted")
	}
}

func TestHashPasswordUniqueSalts(t *testing.T) {
	a, _ := HashPassword("same-password")
	b, _ := HashPassword("same-password")
	if a == b {
		t.Fatal("two hashes identical — salt missing")
	}
}

func TestVerifyPasswordMalformed(t *testing.T) {
	if _, err := VerifyPassword("x", "not-a-hash"); err == nil {
		t.Fatal("malformed hash accepted")
	}
}

func TestBackoffDelayGrowsAndCaps(t *testing.T) {
	if backoffDelay(0) != 0 {
		t.Fatal("no failures should mean no delay")
	}
	d1 := backoffDelay(1)
	d3 := backoffDelay(3)
	d99 := backoffDelay(99)
	if d1 == 0 || d3 <= d1 {
		t.Fatalf("backoff not growing: %v then %v", d1, d3)
	}
	if d99 > 5_000_000_000 { // 5s cap in ns
		t.Fatalf("backoff exceeds cap: %v", d99)
	}
}
