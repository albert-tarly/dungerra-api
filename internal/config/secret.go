package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SecretKeyFileName is the file inside DATA_DIR that holds the generated JWT secret.
const SecretKeyFileName = "secret.key"

// secretKeyBytes is the amount of randomness in a generated key (512 bits,
// matching the `openssl rand -base64 64` the docs suggest for SECRET_KEY).
const secretKeyBytes = 64

// LoadOrCreateSecretKey returns the key stored at path, generating and storing
// a new random one if the file does not exist. The file is readable by the
// owner only. An existing but empty file is an error rather than silently
// replaced, because replacing the key would log out every account.
func LoadOrCreateSecretKey(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		key := strings.TrimSpace(string(data))
		if key == "" {
			return "", fmt.Errorf("%s is empty; delete it to generate a new key", path)
		}
		return key, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	buf := make([]byte, secretKeyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	key := base64.StdEncoding.EncodeToString(buf)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// Write to a temporary file and rename, so a crash never leaves a
	// truncated key behind.
	tmp, err := os.CreateTemp(dir, SecretKeyFileName+".*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	// On Windows this only clears the read-only bit; the ACLs of the per-user
	// data directory protect the file there.
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.WriteString(key + "\n"); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}
	return key, nil
}
