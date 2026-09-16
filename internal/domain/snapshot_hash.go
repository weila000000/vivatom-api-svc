package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func HashSnapshot(snapshot ProjectSnapshot) (string, []byte, error) {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), payload, nil
}
