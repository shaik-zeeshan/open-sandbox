package api

import (
	"errors"
	"testing"
)

func TestNewSandboxSecretEnvCodecFromEnvAcceptsHexEncodedKey(t *testing.T) {
	setSandboxSecretsKey(t, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

	codec, err := newSandboxSecretEnvCodecFromEnv()
	if err != nil {
		t.Fatalf("expected hex key to be accepted, got %v", err)
	}
	if codec == nil {
		t.Fatal("expected codec for hex key")
	}
}

func TestNewSandboxSecretEnvCodecFromEnvRejectsInvalidConfiguredKey(t *testing.T) {
	setSandboxSecretsKey(t, "not-a-valid-key")

	codec, err := newSandboxSecretEnvCodecFromEnv()
	if codec != nil {
		t.Fatal("expected codec to be nil for invalid config")
	}
	if !errors.Is(err, errInvalidSandboxSecretEnvConfig) {
		t.Fatalf("expected invalid config error, got %v", err)
	}
}

func TestSecretEnvHTTPStatusUsesExplicitErrorTypes(t *testing.T) {
	if got := secretEnvHTTPStatus(errSandboxSecretEnvUnavailable); got != 400 {
		t.Fatalf("expected unavailable error to map to 400, got %d", got)
	}
	if got := secretEnvHTTPStatus(errInvalidSandboxSecretEnvEntry); got != 400 {
		t.Fatalf("expected invalid entry error to map to 400, got %d", got)
	}
	if got := secretEnvHTTPStatus(invalidSandboxSecretEnvConfigError(nil)); got != 500 {
		t.Fatalf("expected invalid config error to map to 500, got %d", got)
	}
	if got := secretEnvHTTPStatus(errors.New("decrypt sandbox secret env \"SECRET\": boom")); got != 500 {
		t.Fatalf("expected decrypt failure to map to 500, got %d", got)
	}
}
