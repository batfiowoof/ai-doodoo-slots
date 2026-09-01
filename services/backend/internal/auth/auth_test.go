package auth

import "testing"

func TestNewTokenUniqueAndHashed(t *testing.T) {
	seen := make(map[string]string, 100)
	for i := 0; i < 100; i++ {
		token, hash, err := NewToken()
		if err != nil {
			t.Fatalf("NewToken: %v", err)
		}
		if token == "" || hash == "" {
			t.Fatal("empty token or hash")
		}
		if len(token) < 32 {
			t.Fatalf("token too short: %q", token)
		}
		if len(hash) != 64 {
			t.Fatalf("hash not sha256 hex: %q", hash)
		}
		if prev, dup := seen[hash]; dup {
			t.Fatalf("duplicate hash after %d tokens (collision with %q)", i, prev)
		}
		seen[hash] = token
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	if HashToken("abc") != HashToken("abc") {
		t.Fatal("same token hashed differently")
	}
	if HashToken("abc") == HashToken("abd") {
		t.Fatal("different tokens hashed the same")
	}
}
