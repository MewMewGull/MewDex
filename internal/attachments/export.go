package attachments

import (
	"crypto/sha256"
	"encoding/hex"
)

func ExportID(source, command string) string {
	sum := sha256.Sum256([]byte("mewdex-output\x00" + source + "\x00" + command))
	return hex.EncodeToString(sum[:16])
}

type UnconfirmedDelivery struct{ Cause error }

func (e *UnconfirmedDelivery) Error() string {
	return "file delivery remains unconfirmed: " + e.Cause.Error()
}
func (e *UnconfirmedDelivery) Unwrap() error { return e.Cause }
