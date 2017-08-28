package run

import (
	"encoding/base32"
	"github.com/satori/go.uuid"
	"strings"
)

func GenerateId() string {
	return strings.TrimRight(base32.StdEncoding.EncodeToString(uuid.NewV4().Bytes()), "=")
}
