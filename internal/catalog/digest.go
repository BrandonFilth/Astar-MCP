package catalog

import (
	"crypto/sha256"
	"fmt"
)

func Digest(data []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(data)) }
