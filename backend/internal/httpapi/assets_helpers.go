package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
	"strings"
)

const maxVoiceAssetBytes int64 = 20 * 1024 * 1024

var voiceFileExtensions = map[string]bool{
	".aac":  true,
	".flac": true,
	".m4a":  true,
	".mp3":  true,
	".ogg":  true,
	".opus": true,
	".wav":  true,
	".webm": true,
}

type assetValidationError struct {
	status  int
	message string
}

func (e assetValidationError) Error() string {
	return e.message
}
func validateVoiceAsset(filename, mimeType string, size int64) error {
	if size <= 0 {
		return assetValidationError{status: http.StatusBadRequest, message: "invalid_asset: uploaded audio is empty"}
	}
	if size > maxVoiceAssetBytes {
		return assetValidationError{status: http.StatusBadRequest, message: "invalid_asset: uploaded audio is too large"}
	}
	normalizedMime := strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(normalizedMime, "video/") {
		return assetValidationError{status: http.StatusUnsupportedMediaType, message: "unsupported_media_type: please upload an audio file"}
	}
	if normalizedMime != "" && !strings.HasPrefix(normalizedMime, "audio/") && normalizedMime != "application/ogg" {
		return assetValidationError{status: http.StatusUnsupportedMediaType, message: "unsupported_media_type: please upload an audio file"}
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return assetValidationError{status: http.StatusUnsupportedMediaType, message: "unsupported_media_type: audio extension is required"}
	}
	if !voiceFileExtensions[ext] {
		return assetValidationError{status: http.StatusUnsupportedMediaType, message: "unsupported_media_type: audio extension is not supported"}
	}
	return nil
}
func writeAssetValidationError(w http.ResponseWriter, err error) {
	var validationErr assetValidationError
	if errors.As(err, &validationErr) {
		writeError(w, validationErr.status, validationErr.message)
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}
func mimeTypeFromFilename(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".aac":
		return "audio/aac"
	case ".flac":
		return "audio/flac"
	case ".m4a":
		return "audio/mp4"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".webm":
		return "audio/webm"
	default:
		return "application/octet-stream"
	}
}
func assetMetadataURL(assetID string) string {
	return "/api/v1/assets/" + strings.TrimSpace(assetID)
}
func assetContentURL(assetID string) string {
	return assetMetadataURL(assetID) + "?content=1"
}
func normalizeAssetURLs(asset domain.Asset) domain.Asset {
	asset.ID = strings.TrimSpace(asset.ID)
	if strings.TrimSpace(asset.URL) == "" || strings.Contains(asset.URL, "?content=1") {
		asset.URL = assetMetadataURL(asset.ID)
	}
	if strings.TrimSpace(asset.ContentURL) == "" {
		asset.ContentURL = assetContentURL(asset.ID)
	}
	return asset
}
func hydrateInterviewSubmissionAssets(dataStore store.Store, session *domain.InterviewSession) {
	if dataStore == nil || session == nil {
		return
	}
	for index := range session.Submissions {
		submission := &session.Submissions[index]
		if submission.Asset != nil {
			normalized := normalizeAssetURLs(*submission.Asset)
			submission.Asset = &normalized
			if strings.TrimSpace(submission.AssetID) == "" {
				submission.AssetID = normalized.ID
			}
			submission.AssetURL = normalized.ContentURL
			continue
		}
		if strings.TrimSpace(submission.AssetID) == "" {
			continue
		}
		asset, ok := dataStore.GetAsset(submission.AssetID)
		if !ok {
			continue
		}
		normalized := normalizeAssetURLs(*asset)
		submission.Asset = &normalized
		submission.AssetURL = normalized.ContentURL
	}
}
func localAssetRoot() string {
	if root := strings.TrimSpace(os.Getenv("ASSET_STORAGE_DIR")); root != "" {
		return root
	}
	return filepath.Join(".", "data", "assets")
}
func assetStorageKey(userID, assetID, filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if !voiceFileExtensions[ext] {
		ext = ".bin"
	}
	return filepath.ToSlash(filepath.Join("voice", safePathSegment(userID), assetID+ext))
}
func localAssetPath(storageKey string) (string, error) {
	cleanKey := filepath.Clean(filepath.FromSlash(strings.TrimSpace(storageKey)))
	if cleanKey == "." || cleanKey == "" || filepath.IsAbs(cleanKey) || strings.HasPrefix(cleanKey, "..") {
		return "", errInvalidStorageKey
	}
	root, err := filepath.Abs(localAssetRoot())
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, cleanKey))
	if err != nil {
		return "", err
	}
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", errInvalidStorageKey
	}
	return target, nil
}
func safePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "anonymous"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	return builder.String()
}
