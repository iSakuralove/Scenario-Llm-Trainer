package httpapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"situational-teaching/backend/internal/domain"
)

var (
	errInvalidStorageKey = errors.New("invalid storage key")
	errAssetFileNotFound = errors.New("asset file not found")
)

type readSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

type AssetStorageSaveRequest struct {
	UserID   string
	AssetID  string
	Filename string
	MaxBytes int64
}

type AssetStorageSaveResult struct {
	StorageKey string
	Size       int64
	Checksum   string
}

type AssetStorage interface {
	Save(context.Context, AssetStorageSaveRequest, io.Reader) (AssetStorageSaveResult, error)
	Open(context.Context, *domain.Asset) (readSeekCloser, error)
	Delete(context.Context, *domain.Asset) error
}

type LocalAssetStorage struct{}

func NewAssetStorageFromEnv() AssetStorage {
	driver := strings.ToLower(strings.TrimSpace(os.Getenv("ASSET_STORAGE_DRIVER")))
	switch driver {
	case "", "local", "filesystem", "fs":
		return LocalAssetStorage{}
	default:
		return LocalAssetStorage{}
	}
}

func (LocalAssetStorage) Save(_ context.Context, req AssetStorageSaveRequest, src io.Reader) (AssetStorageSaveResult, error) {
	if strings.TrimSpace(req.AssetID) == "" {
		return AssetStorageSaveResult{}, fmt.Errorf("asset id is required")
	}
	storageKey := assetStorageKey(req.UserID, req.AssetID, req.Filename)
	assetPath, err := localAssetPath(storageKey)
	if err != nil {
		return AssetStorageSaveResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		return AssetStorageSaveResult{}, fmt.Errorf("cannot prepare asset storage: %w", err)
	}
	out, err := os.Create(assetPath)
	if err != nil {
		return AssetStorageSaveResult{}, fmt.Errorf("cannot store uploaded file: %w", err)
	}

	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = maxVoiceAssetBytes
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, hasher), io.LimitReader(src, maxBytes+1))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(assetPath)
		return AssetStorageSaveResult{}, fmt.Errorf("cannot store uploaded file")
	}
	if written <= 0 {
		_ = os.Remove(assetPath)
		return AssetStorageSaveResult{}, assetValidationError{status: http.StatusBadRequest, message: "invalid_asset: uploaded audio is empty"}
	}
	if written > maxBytes {
		_ = os.Remove(assetPath)
		return AssetStorageSaveResult{}, assetValidationError{status: http.StatusBadRequest, message: "invalid_asset: uploaded audio is too large"}
	}

	return AssetStorageSaveResult{
		StorageKey: storageKey,
		Size:       written,
		Checksum:   fmt.Sprintf("%x", hasher.Sum(nil)),
	}, nil
}

func (LocalAssetStorage) Open(_ context.Context, asset *domain.Asset) (readSeekCloser, error) {
	if asset == nil {
		return nil, fmt.Errorf("asset is required")
	}
	assetPath, err := localAssetPath(asset.StorageKey)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(assetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errAssetFileNotFound
		}
		return nil, err
	}
	return file, nil
}

func (LocalAssetStorage) Delete(_ context.Context, asset *domain.Asset) error {
	if asset == nil {
		return nil
	}
	assetPath, err := localAssetPath(asset.StorageKey)
	if err != nil {
		return err
	}
	if err := os.Remove(assetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func writeAssetStorageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvalidStorageKey):
		writeError(w, http.StatusBadRequest, "invalid_asset: invalid storage key")
	case errors.Is(err, errAssetFileNotFound):
		writeError(w, http.StatusNotFound, "asset file not found")
	default:
		var validationErr assetValidationError
		if errors.As(err, &validationErr) {
			writeError(w, validationErr.status, validationErr.message)
			return
		}
		writeError(w, http.StatusInternalServerError, "invalid_asset: cannot store uploaded file")
	}
}
