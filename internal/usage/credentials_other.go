//go:build !darwin

package usage

import "os"

func claudeCredentials() ([]byte, error) { return os.ReadFile(credentialsPath()) }
