package credential

import (
	"errors"
	"testing"
)

func TestStoreGetRemoveWithoutKeychain(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AGENT_FIGMA_NO_KEYCHAIN", "1")

	storage, err := Store("work", "fake-token-value")
	if err != nil {
		t.Fatal(err)
	}
	if storage != "file" {
		t.Fatalf("storage = %q, want file when the keychain is opted out", storage)
	}
	if got, err := Get("work"); err != nil || got != "fake-token-value" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if Storage("work") != "file" || Storage("nope") != "missing" {
		t.Fatal("unexpected storage report")
	}
	if err := Remove("work"); err != nil {
		t.Fatal(err)
	}
	var notFound *NotFoundError
	if _, err := Get("work"); !errors.As(err, &notFound) {
		t.Fatalf("Get after Remove = %v, want NotFoundError", err)
	}
}
