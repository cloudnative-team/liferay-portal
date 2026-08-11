package addon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemStore(t *testing.T) {
	content := []byte("PK\x03\x04 fake lpkg bytes")
	contentChecksum := checksum(content)

	t.Run("reports not held when the file is absent", func(t *testing.T) {
		store := NewFilesystemStore(t.TempDir())

		has, error := store.Has(contentChecksum, 1)

		if error != nil {
			t.Fatalf("Unexpected error: %v", error)
		}

		if has {
			t.Error("Has = true, want false for an absent file")
		}
	})

	t.Run("saves then reports held for a matching checksum", func(t *testing.T) {
		directory := t.TempDir()
		store := NewFilesystemStore(directory)

		if error := store.Save(contentChecksum, bytes.NewReader(content), 1); error != nil {
			t.Fatalf("Unexpected error: %v", error)
		}

		if _, error := os.Stat(filepath.Join(directory, "1.lpkg")); error != nil {
			t.Fatalf("Expected 1.lpkg on disk: %v", error)
		}

		has, error := store.Has(contentChecksum, 1)

		if error != nil {
			t.Fatalf("Unexpected error: %v", error)
		}

		if !has {
			t.Error("Has = false, want true after a matching save")
		}
	})

	t.Run("reports not held when the checksum differs", func(t *testing.T) {
		store := NewFilesystemStore(t.TempDir())

		if error := store.Save(contentChecksum, bytes.NewReader(content), 1); error != nil {
			t.Fatalf("Unexpected error: %v", error)
		}

		has, error := store.Has(checksum([]byte("other")), 1)

		if error != nil {
			t.Fatalf("Unexpected error: %v", error)
		}

		if has {
			t.Error("Has = true, want false for a differing checksum")
		}
	})

	t.Run("rejects a checksum mismatch and writes no file", func(t *testing.T) {
		directory := t.TempDir()
		store := NewFilesystemStore(directory)

		error := store.Save(checksum([]byte("wrong")), bytes.NewReader(content), 1)

		if error == nil {
			t.Fatal("Expected a checksum mismatch error, got nil")
		}

		entries, _ := os.ReadDir(directory)

		if len(entries) != 0 {
			t.Errorf("Expected no files after a rejected save, got %d", len(entries))
		}
	})

	t.Run("overwrites an existing file on a second save", func(t *testing.T) {
		directory := t.TempDir()
		store := NewFilesystemStore(directory)

		updated := []byte("PK\x03\x04 newer lpkg bytes")

		if error := store.Save(contentChecksum, bytes.NewReader(content), 1); error != nil {
			t.Fatalf("Unexpected error: %v", error)
		}

		if error := store.Save(checksum(updated), bytes.NewReader(updated), 1); error != nil {
			t.Fatalf("Unexpected error: %v", error)
		}

		has, error := store.Has(checksum(updated), 1)

		if error != nil {
			t.Fatalf("Unexpected error: %v", error)
		}

		if !has {
			t.Error("Has = false, want true for the updated content")
		}
	})
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}
