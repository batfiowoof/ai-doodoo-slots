package wallet

import "fmt"

// TopupKey builds the hourly deposit idempotency key: one top-up per user
// per UTC hour, replay-safe through the ledger's unique key.
func TopupKey(userID int64, hourBucket int64) string {
	return fmt.Sprintf("topup:%d:%d", userID, hourBucket)
}
