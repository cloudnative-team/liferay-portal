package addon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

type Store interface {
	Has(checksum string, virtualEntryID int64) (bool, error)
	Save(expectedChecksum string, reader io.Reader, virtualEntryID int64) error
}

type FilesystemStore struct {
	directory string
}

func NewFilesystemStore(directory string) *FilesystemStore {
	return &FilesystemStore{directory: directory}
}

func (filesystemStore *FilesystemStore) Has(
	checksum string, virtualEntryID int64,
) (bool, error) {
	file, error := os.Open(filesystemStore.path(virtualEntryID))

	if os.IsNotExist(error) {
		return false, nil
	}

	if error != nil {
		return false, error
	}

	defer file.Close()

	actualChecksum, error := computeChecksum(file)

	if error != nil {
		return false, error
	}

	return actualChecksum == checksum, nil
}

func (filesystemStore *FilesystemStore) Save(
	expectedChecksum string, reader io.Reader, virtualEntryID int64,
) error {
	if error := os.MkdirAll(filesystemStore.directory, 0o755); error != nil {
		return error
	}

	temporaryFile, error := os.CreateTemp(
		filesystemStore.directory, fmt.Sprintf("%d.lpkg.*", virtualEntryID),
	)

	if error != nil {
		return error
	}

	temporaryPath := temporaryFile.Name()

	hash := sha256.New()

	_, error = io.Copy(temporaryFile, io.TeeReader(reader, hash))

	if closeError := temporaryFile.Close(); error == nil {
		error = closeError
	}

	if error != nil {
		os.Remove(temporaryPath)

		return error
	}

	actualChecksum := hex.EncodeToString(hash.Sum(nil))

	if actualChecksum != expectedChecksum {
		os.Remove(temporaryPath)

		return fmt.Errorf(
			"addon store: checksum mismatch for virtual entry %d: got %s, want %s",
			virtualEntryID, actualChecksum, expectedChecksum,
		)
	}

	return os.Rename(temporaryPath, filesystemStore.path(virtualEntryID))
}

func (filesystemStore *FilesystemStore) path(virtualEntryID int64) string {
	return filepath.Join(
		filesystemStore.directory, strconv.FormatInt(virtualEntryID, 10)+".lpkg",
	)
}

func computeChecksum(reader io.Reader) (string, error) {
	hash := sha256.New()

	if _, error := io.Copy(hash, reader); error != nil {
		return "", error
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
