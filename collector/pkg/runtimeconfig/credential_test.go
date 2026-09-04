package runtimeconfig

import (
	"context"
	"testing"
)

func TestMemorySecretStoreRoundTrip(t *testing.T) {
	store := NewMemorySecretStore()
	value, found, err := store.Read(context.Background())
	if err != nil || found || value != "" {
		t.Fatalf("initial memory store state was unexpected: found=%t err=%v", found, err)
	}
	if err := store.Write(context.Background(), "synthetic-secret"); err != nil {
		t.Fatal(err)
	}
	value, found, err = store.Read(context.Background())
	if err != nil || !found || value != "synthetic-secret" {
		t.Fatalf("written memory store state was unexpected: found=%t err=%v", found, err)
	}
	if err := store.Write(context.Background(), "updated-secret"); err != nil {
		t.Fatal(err)
	}
	value, found, err = store.Read(context.Background())
	if err != nil || !found || value != "updated-secret" {
		t.Fatalf("updated memory store state was unexpected: found=%t err=%v", found, err)
	}
	if err := store.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Read(context.Background()); err != nil || found {
		t.Fatalf("deleted memory store found=%t err=%v", found, err)
	}
}

func TestCredentialTargetValidationRejectsNonTestNames(t *testing.T) {
	for _, target := range []string{
		"",
		"ProxyLens/Test/not-a-uuid",
		"ProxyLens/Other/00000000-0000-4000-8000-000000000000",
	} {
		if err := ValidateTestCredentialTarget(target); err == nil {
			t.Errorf("invalid test credential target %q unexpectedly accepted", target)
		}
	}
}
