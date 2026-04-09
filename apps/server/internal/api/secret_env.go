package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/shaik-zeeshan/open-sandbox/internal/store"
)

const sandboxSecretsKeyEnvVar = "SANDBOX_SECRETS_KEY"

var errSandboxSecretEnvUnavailable = errors.New("sandbox secret env is unavailable: SANDBOX_SECRETS_KEY is not configured")
var errInvalidSandboxSecretEnvEntry = errors.New("invalid sandbox secret env entry")
var errInvalidSandboxSecretEnvConfig = errors.New("invalid sandbox secret env config")

type sandboxSecretEnvCodec struct {
	aead cipher.AEAD
}

type sandboxSecretEnvState struct {
	EncryptedEnv []string
	Keys         []string
	RuntimeEnv   []string
}

func newSandboxSecretEnvCodecFromEnv() (*sandboxSecretEnvCodec, error) {
	raw, ok := os.LookupEnv(sandboxSecretsKeyEnvVar)
	if !ok {
		return nil, nil
	}

	key := strings.TrimSpace(raw)
	if key == "" {
		return nil, invalidSandboxSecretEnvConfigError(nil)
	}

	decoded, err := decodeSandboxSecretKey(key)
	if err != nil {
		return nil, invalidSandboxSecretEnvConfigError(err)
	}
	block, err := aes.NewCipher(decoded)
	if err != nil {
		return nil, invalidSandboxSecretEnvConfigError(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, invalidSandboxSecretEnvConfigError(err)
	}
	return &sandboxSecretEnvCodec{aead: aead}, nil
}

func decodeSandboxSecretKey(raw string) ([]byte, error) {
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, errors.New("invalid sandbox secrets key")
}

func secretEnvHTTPStatus(err error) int {
	if errors.Is(err, errSandboxSecretEnvUnavailable) || errors.Is(err, errInvalidSandboxSecretEnvEntry) {
		return 400
	}
	return 500
}

func (s *Server) encryptSandboxSecretEnv(entries []string) (sandboxSecretEnvState, error) {
	parsed, err := parseSandboxEnvEntries(entries)
	if err != nil {
		return sandboxSecretEnvState{}, err
	}
	if len(parsed) == 0 {
		return sandboxSecretEnvState{}, nil
	}
	if s.secretEnvCodec == nil {
		return sandboxSecretEnvState{}, sandboxSecretEnvUnavailableError()
	}
	return s.secretEnvCodec.encrypt(parsed)
}

func (s *Server) resolveSandboxSecretEnvState(sandbox store.Sandbox, upserts []string, removeKeys []string) (sandboxSecretEnvState, error) {
	existing, err := s.decryptSandboxSecretEnv(sandbox.SecretEnv)
	if err != nil {
		return sandboxSecretEnvState{}, err
	}
	for _, key := range removeKeys {
		delete(existing, strings.TrimSpace(key))
	}
	parsedUpserts, err := parseSandboxEnvEntries(upserts)
	if err != nil {
		return sandboxSecretEnvState{}, err
	}
	for key, value := range parsedUpserts {
		existing[key] = value
	}
	if len(existing) == 0 {
		return sandboxSecretEnvState{}, nil
	}
	if s.secretEnvCodec == nil {
		return sandboxSecretEnvState{}, sandboxSecretEnvUnavailableError()
	}
	return s.secretEnvCodec.encrypt(existing)
}

func (s *Server) decryptSandboxSecretEnv(entries []string) (map[string]string, error) {
	if len(entries) == 0 {
		return map[string]string{}, nil
	}
	if s.secretEnvCodec == nil {
		return nil, sandboxSecretEnvUnavailableError()
	}
	decrypted, err := s.secretEnvCodec.decrypt(entries)
	if err != nil {
		return nil, err
	}
	return decrypted, nil
}

func (c *sandboxSecretEnvCodec) encrypt(values map[string]string) (sandboxSecretEnvState, error) {
	keys := sortedSandboxEnvKeys(values)
	encrypted := make([]string, 0, len(keys))
	runtime := make([]string, 0, len(keys))
	for _, key := range keys {
		ciphertext, err := c.encryptValue(values[key])
		if err != nil {
			return sandboxSecretEnvState{}, fmt.Errorf("encrypt sandbox secret env: %w", err)
		}
		encrypted = append(encrypted, key+"="+ciphertext)
		runtime = append(runtime, key+"="+values[key])
	}
	return sandboxSecretEnvState{EncryptedEnv: encrypted, Keys: keys, RuntimeEnv: runtime}, nil
}

func (c *sandboxSecretEnvCodec) decrypt(entries []string) (map[string]string, error) {
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, encoded, err := splitSandboxEnvEntry(entry)
		if err != nil {
			return nil, err
		}
		plaintext, err := c.decryptValue(encoded)
		if err != nil {
			return nil, fmt.Errorf("decrypt sandbox secret env %q: %w", key, err)
		}
		values[key] = plaintext
	}
	return values, nil
}

func (c *sandboxSecretEnvCodec) encryptValue(value string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(value), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *sandboxSecretEnvCodec) decryptValue(value string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	nonceSize := c.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("ciphertext is too short")
	}
	plaintext, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func parseSandboxEnvEntries(entries []string) (map[string]string, error) {
	parsed := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, err := splitSandboxEnvEntry(entry)
		if err != nil {
			return nil, err
		}
		parsed[key] = value
	}
	return parsed, nil
}

func splitSandboxEnvEntry(entry string) (string, string, error) {
	key, value, ok := strings.Cut(entry, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return "", "", fmt.Errorf("%w %q: expected KEY=VALUE", errInvalidSandboxSecretEnvEntry, entry)
	}
	return key, value, nil
}

func sandboxSecretEnvUnavailableError() error {
	if _, ok := os.LookupEnv(sandboxSecretsKeyEnvVar); !ok {
		return errSandboxSecretEnvUnavailable
	}
	return invalidSandboxSecretEnvConfigError(nil)
}

func invalidSandboxSecretEnvConfigError(err error) error {
	msg := "sandbox secret env is unavailable: SANDBOX_SECRETS_KEY must be a raw 32-byte string or base64-encoded 32-byte key"
	if err == nil {
		return fmt.Errorf("%w: %s", errInvalidSandboxSecretEnvConfig, msg)
	}
	return fmt.Errorf("%w: %s: %v", errInvalidSandboxSecretEnvConfig, msg, err)
}

func ValidateConfigFromEnv() error {
	_, err := newSandboxSecretEnvCodecFromEnv()
	return err
}

func sortedSandboxEnvKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
