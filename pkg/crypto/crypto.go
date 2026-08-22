package crypto

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"filippo.io/age"
	"filippo.io/age/armor"
)

// GenerateMasterKey creates a new random 256-bit X25519 keypair for the vault.
func GenerateMasterKey() (identityStr string, recipientStr string, err error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate X25519 identity: %w", err)
	}
	return identity.String(), identity.Recipient().String(), nil
}

// EncryptWithKey encrypts plaintext with a fast X25519 recipient key in microseconds.
func EncryptWithKey(plaintext []byte, recipientStr string) ([]byte, error) {
	recipient, err := age.ParseX25519Recipient(recipientStr)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient key: %w", err)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, fmt.Errorf("failed to create age encryptor: %w", err)
	}

	if _, err := io.Copy(w, bytes.NewReader(plaintext)); err != nil {
		return nil, fmt.Errorf("failed to write plaintext: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize encryption: %w", err)
	}

	return buf.Bytes(), nil
}

// DecryptWithKey decrypts ciphertext with a fast X25519 identity key in microseconds.
func DecryptWithKey(ciphertext []byte, identityStr string) ([]byte, error) {
	identity, err := age.ParseX25519Identity(identityStr)
	if err != nil {
		return nil, fmt.Errorf("invalid identity key: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (corrupted data or wrong key): %w", err)
	}

	var out bytes.Buffer
	if _, err := io.Copy(&out, r); err != nil {
		return nil, fmt.Errorf("failed to read decrypted stream: %w", err)
	}

	return out.Bytes(), nil
}

// EncryptFileWithKey encrypts a file on disk using the fast master key atomically.
func EncryptFileWithKey(srcPath, dstPath string, recipientStr string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read source file %s: %w", srcPath, err)
	}

	encryptedData, err := EncryptWithKey(data, recipientStr)
	if err != nil {
		return fmt.Errorf("encryption error for %s: %w", srcPath, err)
	}

	tmpDst := dstPath + ".tmp"
	if err := os.WriteFile(tmpDst, encryptedData, 0600); err != nil {
		return fmt.Errorf("failed to write encrypted temp file: %w", err)
	}

	if err := os.Rename(tmpDst, dstPath); err != nil {
		_ = os.Remove(tmpDst)
		return fmt.Errorf("failed to commit encrypted file %s: %w", dstPath, err)
	}

	return nil
}

// DecryptFileWithKey decrypts an age file on disk using the fast master key atomically.
func DecryptFileWithKey(srcPath, dstPath string, identityStr string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read encrypted file %s: %w", srcPath, err)
	}

	decryptedData, err := DecryptWithKey(data, identityStr)
	if err != nil {
		return fmt.Errorf("decryption error for %s: %w", srcPath, err)
	}

	tmpDst := dstPath + ".tmp"
	if err := os.WriteFile(tmpDst, decryptedData, 0600); err != nil {
		return fmt.Errorf("failed to write decrypted temp file: %w", err)
	}

	if err := os.Rename(tmpDst, dstPath); err != nil {
		_ = os.Remove(tmpDst)
		return fmt.Errorf("failed to commit decrypted file %s: %w", dstPath, err)
	}

	return nil
}

// Encrypt encrypts plaintext bytes with a passphrase using Scrypt KDF (used for Master Key wrapping).
func Encrypt(plaintext []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("passphrase cannot be empty")
	}

	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to create scrypt recipient: %w", err)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, fmt.Errorf("failed to create age encryptor: %w", err)
	}

	if _, err := io.Copy(w, bytes.NewReader(plaintext)); err != nil {
		return nil, fmt.Errorf("failed to write plaintext: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize encryption: %w", err)
	}

	return buf.Bytes(), nil
}

// Decrypt decrypts age ciphertext bytes with a passphrase.
func Decrypt(ciphertext []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("passphrase cannot be empty")
	}

	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to create scrypt identity: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong passphrase or corrupted data): %w", err)
	}

	var out bytes.Buffer
	if _, err := io.Copy(&out, r); err != nil {
		return nil, fmt.Errorf("failed to read decrypted stream: %w", err)
	}

	return out.Bytes(), nil
}

// EncryptFile encrypts a file on disk to a destination path atomically with passphrase.
func EncryptFile(srcPath, dstPath string, passphrase string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read source file %s: %w", srcPath, err)
	}

	encryptedData, err := Encrypt(data, passphrase)
	if err != nil {
		return fmt.Errorf("encryption error for %s: %w", srcPath, err)
	}

	tmpDst := dstPath + ".tmp"
	if err := os.WriteFile(tmpDst, encryptedData, 0600); err != nil {
		return fmt.Errorf("failed to write encrypted temp file: %w", err)
	}

	if err := os.Rename(tmpDst, dstPath); err != nil {
		_ = os.Remove(tmpDst)
		return fmt.Errorf("failed to commit encrypted file %s: %w", dstPath, err)
	}

	return nil
}

// DecryptFile decrypts an age file on disk to a destination path atomically with passphrase.
func DecryptFile(srcPath, dstPath string, passphrase string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read encrypted file %s: %w", srcPath, err)
	}

	decryptedData, err := Decrypt(data, passphrase)
	if err != nil {
		return fmt.Errorf("decryption error for %s: %w", srcPath, err)
	}

	tmpDst := dstPath + ".tmp"
	if err := os.WriteFile(tmpDst, decryptedData, 0600); err != nil {
		return fmt.Errorf("failed to write decrypted temp file: %w", err)
	}

	if err := os.Rename(tmpDst, dstPath); err != nil {
		_ = os.Remove(tmpDst)
		return fmt.Errorf("failed to commit decrypted file %s: %w", dstPath, err)
	}

	return nil
}

// EncryptArmored encrypts plaintext with armor ASCII formatting.
func EncryptArmored(plaintext []byte, passphrase string) (string, error) {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	armorWriter := armor.NewWriter(&buf)
	w, err := age.Encrypt(armorWriter, recipient)
	if err != nil {
		return "", err
	}

	if _, err := io.Copy(w, bytes.NewReader(plaintext)); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	if err := armorWriter.Close(); err != nil {
		return "", err
	}

	return buf.String(), nil
}
