//go:build darwin && !cgo

package usage

import "os"

func claudeCredentials() ([]byte, error) { return os.ReadFile(credentialsPath()) }
