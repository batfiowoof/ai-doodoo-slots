package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters, tuned so a hash costs roughly 100ms. They are stored
// inside the encoded hash so they can be raised later without invalidating
// old hashes.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB (64 MiB)
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

var ErrMalformedHash = errors.New("malformed password hash")

// HashPassword returns a PHC-format argon2id hash:
// $argon2id$v=19$m=65536,t=3,p=2$salt$hash
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword parses the PHC string, recomputes the key with the stored
// parameters, and compares in constant time.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrMalformedHash
	}
	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, ErrMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrMalformedHash
	}
	computed := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(computed, key) == 1, nil
}

// DummyHash burns one hash worth of time when the account does not exist so
// login timing does not reveal whether an email is registered.
func DummyHash() {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return // timing equalization is best-effort on entropy failure
	}
	argon2.IDKey([]byte("dummy-password-for-timing"), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
}
