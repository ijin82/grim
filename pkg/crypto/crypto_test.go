package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestMasterKeyEncryptDecrypt(t *testing.T) {
	privKey, pubKey, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey failed: %v", err)
	}

	plaintext := []byte("## Microsecond Encryption\nSpeed: 1000x faster than Scrypt per file")
	ciphertext, err := EncryptWithKey(plaintext, pubKey)
	if err != nil {
		t.Fatalf("EncryptWithKey failed: %v", err)
	}

	decrypted, err := DecryptWithKey(ciphertext, privKey)
	if err != nil {
		t.Fatalf("DecryptWithKey failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("Decrypted mismatch. Got: %s, Want: %s", decrypted, plaintext)
	}
}

func TestEncryptDecrypt(t *testing.T) {
	passphrase := "super-secret-master-passphrase-123!"
	plaintext := []byte("## Secret Server Notes\nHost: 192.168.1.100\nKey: ssh-ed25519 AAAA...")

	ciphertext, err := Encrypt(plaintext, passphrase)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Fatalf("Ciphertext equals plaintext")
	}

	decrypted, err := Decrypt(ciphertext, passphrase)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("Decrypted does not match plaintext. Got: %s, Want: %s", decrypted, plaintext)
	}
}

func TestWrongPassphrase(t *testing.T) {
	passphrase := "correct-passphrase"
	wrongPassphrase := "wrong-passphrase"
	plaintext := []byte("Confidential data")

	ciphertext, err := Encrypt(plaintext, passphrase)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	_, err = Decrypt(ciphertext, wrongPassphrase)
	if err == nil {
		t.Fatalf("Expected error when decrypting with wrong passphrase, but got nil")
	}
}

func TestCorruptedData(t *testing.T) {
	passphrase := "correct-passphrase"
	plaintext := []byte("Confidential data")

	ciphertext, err := Encrypt(plaintext, passphrase)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Corrupt some bytes
	if len(ciphertext) > 50 {
		ciphertext[45] ^= 0xFF
	}

	_, err = Decrypt(ciphertext, passphrase)
	if err == nil {
		t.Fatalf("Expected error when decrypting corrupted ciphertext, but got nil")
	}
}

func TestEncryptDecryptFile(t *testing.T) {
	tempDir := t.TempDir()
	srcFile := filepath.Join(tempDir, "note.md")
	encFile := filepath.Join(tempDir, "note.md.age")
	decFile := filepath.Join(tempDir, "note_dec.md")

	content := []byte("# Production Database Credentials\nUser: admin\nPass: $ecr3t")
	if err := os.WriteFile(srcFile, content, 0600); err != nil {
		t.Fatalf("Failed to write src file: %v", err)
	}

	passphrase := "file-passphrase"

	if err := EncryptFile(srcFile, encFile, passphrase); err != nil {
		t.Fatalf("EncryptFile failed: %v", err)
	}

	if err := DecryptFile(encFile, decFile, passphrase); err != nil {
		t.Fatalf("DecryptFile failed: %v", err)
	}

	decryptedContent, err := os.ReadFile(decFile)
	if err != nil {
		t.Fatalf("Failed to read decrypted file: %v", err)
	}

	if !bytes.Equal(decryptedContent, content) {
		t.Fatalf("Decrypted file content mismatch. Got: %s, Want: %s", decryptedContent, content)
	}
}
