package crypto

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

// encryptReaderToFile streams encrypted data into a temporary file and commits
// it with an atomic rename. The source is never loaded into memory as a whole.
func encryptReaderToFile(src io.Reader, dstPath string, recipient age.Recipient) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0700); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(dstPath), ".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create encrypted temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	writer, err := age.Encrypt(tmpFile, recipient)
	if err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to create age encryptor: %w", err)
	}

	if _, err := io.Copy(writer, src); err != nil {
		_ = writer.Close()
		_ = tmpFile.Close()
		return fmt.Errorf("failed to stream plaintext: %w", err)
	}
	if err := writer.Close(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to finalize encryption: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync encrypted file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close encrypted file: %w", err)
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return fmt.Errorf("failed to commit encrypted file %s: %w", dstPath, err)
	}

	return nil
}

// decryptReaderToFile streams decrypted data into a temporary file and commits
// it with an atomic rename.
func decryptReaderToFile(src io.Reader, dstPath string, identity age.Identity) error {
	reader, err := age.Decrypt(src, identity)
	if err != nil {
		return fmt.Errorf("decryption failed (corrupted data or wrong key): %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0700); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(dstPath), ".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create decrypted temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmpFile, reader); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to stream decrypted data: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync decrypted file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close decrypted file: %w", err)
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return fmt.Errorf("failed to commit decrypted file %s: %w", dstPath, err)
	}

	return nil
}

// EncryptFileWithKey encrypts a file on disk using the fast master key atomically.
func EncryptFileWithKey(srcPath, dstPath string, recipientStr string) error {
	recipient, err := age.ParseX25519Recipient(recipientStr)
	if err != nil {
		return fmt.Errorf("invalid recipient key: %w", err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", srcPath, err)
	}
	defer func() { _ = src.Close() }()

	if err := encryptReaderToFile(src, dstPath, recipient); err != nil {
		return fmt.Errorf("encryption error for %s: %w", srcPath, err)
	}
	return nil
}

// DecryptFileWithKey decrypts an age file on disk using the fast master key atomically.
func DecryptFileWithKey(srcPath, dstPath string, identityStr string) error {
	identity, err := age.ParseX25519Identity(identityStr)
	if err != nil {
		return fmt.Errorf("invalid identity key: %w", err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open encrypted file %s: %w", srcPath, err)
	}
	defer func() { _ = src.Close() }()

	if err := decryptReaderToFile(src, dstPath, identity); err != nil {
		return fmt.Errorf("decryption error for %s: %w", srcPath, err)
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
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return fmt.Errorf("failed to create scrypt recipient: %w", err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", srcPath, err)
	}
	defer func() { _ = src.Close() }()

	if err := encryptReaderToFile(src, dstPath, recipient); err != nil {
		return fmt.Errorf("encryption error for %s: %w", srcPath, err)
	}
	return nil
}

// DecryptFile decrypts an age file on disk to a destination path atomically with passphrase.
func DecryptFile(srcPath, dstPath string, passphrase string) error {
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return fmt.Errorf("failed to create scrypt identity: %w", err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open encrypted file %s: %w", srcPath, err)
	}
	defer func() { _ = src.Close() }()

	if err := decryptReaderToFile(src, dstPath, identity); err != nil {
		return fmt.Errorf("decryption error for %s: %w", srcPath, err)
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
