//go:build windows

package runtimeconfig

import (
	"context"
	"testing"
)

func TestWindowsCredentialStoreUsesOnlyRandomTestTarget(t *testing.T) {
	target, err := NewTestCredentialTarget()
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewCredentialStoreForTarget(target)
	if err != nil {
		t.Fatalf("NewCredentialStoreForTarget failed: %v", err)
	}
	ctx := context.Background()
	_, found, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("initial exact test-target read failed: %v", err)
	}
	if found {
		t.Fatalf("random test credential target was already present: %s", target)
	}
	cleaned := false
	t.Cleanup(func() {
		if cleaned {
			return
		}
		if err := store.Delete(ctx); err != nil {
			t.Errorf("failed to delete exact test credential target %s: %v", target, err)
		}
	})
	if err := store.Write(ctx, "synthetic-wincred-secret"); err != nil {
		t.Fatalf("CredWriteW test-target write failed: %v", err)
	}
	value, found, err := store.Read(ctx)
	if err != nil || !found || value != "synthetic-wincred-secret" {
		t.Fatalf("CredReadW round trip value=%q found=%t err=%v", value, found, err)
	}
	if err := store.Write(ctx, "synthetic-wincred-updated"); err != nil {
		t.Fatalf("CredWriteW overwrite failed: %v", err)
	}
	value, found, err = store.Read(ctx)
	if err != nil || !found || value != "synthetic-wincred-updated" {
		t.Fatalf("CredReadW overwrite value=%q found=%t err=%v", value, found, err)
	}
	if err := store.Delete(ctx); err != nil {
		t.Fatalf("CredDeleteW exact test-target cleanup failed: %v", err)
	}
	cleaned = true
	if _, found, err := store.Read(ctx); err != nil || found {
		t.Fatalf("post-delete exact test-target read found=%t err=%v", found, err)
	}
	if err := store.Delete(ctx); err != nil {
		t.Fatalf("idempotent exact test-target delete failed: %v", err)
	}
}
