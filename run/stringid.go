package run

import (
	"encoding/base32"
	"strings"

	"github.com/satori/go.uuid"
)

func GenerateId() string {
	return strings.TrimRight(base32.StdEncoding.EncodeToString(uuid.NewV4().Bytes()), "=")
}
