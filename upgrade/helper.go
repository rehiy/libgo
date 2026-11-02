package upgrade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

func parseChecksum(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, err
	}
	if len(decoded) != sha256.Size {
		return nil, errors.New("invalid checksum length")
	}
	return decoded, nil
}

func verifyChecksum(filePath string, checksum []byte) error {
	if len(checksum) == 0 {
		return nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}

	if !bytes.Equal(checksum, hash.Sum(nil)) {
		return errors.New("updated file has wrong checksum")
	}

	return nil
}

func commitBinary(newBinary, targetPath string, targetMode os.FileMode, oldVersion string) error {
	if err := verifyChecksum(newBinary, nil); err != nil {
		return err
	}

	if err := os.Chmod(newBinary, targetMode); err != nil {
		return err
	}

	backupPath := targetPath + "-" + oldVersion
	var targetMoved bool

	if _, err := os.Stat(targetPath); err == nil {
		if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(targetPath, backupPath); err != nil {
			return err
		}
		targetMoved = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Rename(newBinary, targetPath); err != nil {
		if targetMoved {
			if rollbackErr := os.Rename(backupPath, targetPath); rollbackErr != nil {
				return &ErrRollback{Err: err, Rollback: rollbackErr}
			}
		}
		return err
	}

	if targetMoved {
		_ = os.Remove(backupPath)
	}

	return nil
}
