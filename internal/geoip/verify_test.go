package geoip

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySHA256_Pass(t *testing.T) {
	dir := t.TempDir()
	content := []byte("test geoip database content")
	fpath := filepath.Join(dir, "test.db")
	if err := os.WriteFile(fpath, content, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	h := sha256.Sum256(content)
	expected := hex.EncodeToString(h[:])

	if err := VerifySHA256(fpath, expected); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

func TestVerifySHA256_PassWithPrefix(t *testing.T) {
	dir := t.TempDir()
	content := []byte("test data with prefix")
	fpath := filepath.Join(dir, "test.db")
	if err := os.WriteFile(fpath, content, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	h := sha256.Sum256(content)
	expected := "sha256:" + hex.EncodeToString(h[:])

	if err := VerifySHA256(fpath, expected); err != nil {
		t.Fatalf("verify with prefix failed: %v", err)
	}
}

func TestVerifySHA256_Fail(t *testing.T) {
	dir := t.TempDir()
	content := []byte("original content")
	fpath := filepath.Join(dir, "test.db")
	if err := os.WriteFile(fpath, content, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	err := VerifySHA256(fpath, wrongHash)
	if err == nil {
		t.Fatal("expected verification error")
	}
}

func TestComputeSHA256Hex(t *testing.T) {
	dir := t.TempDir()
	content := []byte("compute this hash")
	fpath := filepath.Join(dir, "test.db")
	if err := os.WriteFile(fpath, content, 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	got, err := ComputeSHA256Hex(fpath)
	if err != nil {
		t.Fatalf("ComputeSHA256Hex failed: %v", err)
	}

	h := sha256.Sum256(content)
	expected := hex.EncodeToString(h[:])
	if got != expected {
		t.Fatalf("hash mismatch: got %s, want %s", got, expected)
	}
}

func TestComputeSHA256Hex_FileNotFound(t *testing.T) {
	_, err := ComputeSHA256Hex("/nonexistent/path/to/file.db")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}
