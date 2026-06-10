package httpapi

import (
	"fmt"
	"net/http"
	"path/filepath"
	"situational-teaching/backend/internal/domain"
	"situational-teaching/backend/internal/store"
	"strings"
)

func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	parts := split(suffix)
	if len(parts) == 0 && r.Method == http.MethodPost {
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
			s.handleAssetUpload(w, r, user)
			return
		}
		s.handleAssetMetadataCreate(w, r, user)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		asset, ok := s.store.GetAsset(parts[0])
		if !ok || (asset.UserID != user.ID && user.Role != domain.RoleAdmin) {
			writeError(w, http.StatusNotFound, "asset not found")
			return
		}
		normalized := normalizeAssetURLs(*asset)
		if r.URL.Query().Get("content") == "1" || r.URL.Query().Get("download") == "1" {
			s.serveAssetContent(w, r, &normalized)
			return
		}
		writeOK(w, normalized)
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}
func (s *Server) handleAssetMetadataCreate(w http.ResponseWriter, r *http.Request, user *domain.User) {
	var req struct {
		Kind     string `json:"kind"`
		Filename string `json:"filename"`
		MimeType string `json:"mime_type"`
		Size     int64  `json:"size"`
		Checksum string `json:"checksum"`
	}
	if !decode(w, r, &req) {
		return
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "voice"
	}
	mimeType := strings.TrimSpace(req.MimeType)
	if kind == "voice" {
		if err := validateVoiceAsset(strings.TrimSpace(req.Filename), mimeType, req.Size); err != nil {
			writeAssetValidationError(w, err)
			return
		}
	}
	asset, err := s.store.CreateAsset(domain.Asset{
		UserID:   user.ID,
		Kind:     kind,
		Filename: strings.TrimSpace(req.Filename),
		MimeType: mimeType,
		Size:     req.Size,
		Checksum: strings.TrimSpace(req.Checksum),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	asset = normalizeAssetURLs(asset)
	s.audit(r, user, "asset.create", "asset", asset.ID, map[string]string{"kind": asset.Kind, "mode": "metadata"})
	writeOK(w, asset)
}
func (s *Server) handleAssetUpload(w http.ResponseWriter, r *http.Request, user *domain.User) {
	if err := r.ParseMultipartForm(maxVoiceAssetBytes + 1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_asset: cannot read uploaded file")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		file, header, err = r.FormFile("asset")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_asset: audio file is required")
		return
	}
	defer file.Close()

	kind := strings.TrimSpace(r.FormValue("kind"))
	if kind == "" {
		kind = "voice"
	}
	filename := strings.TrimSpace(header.Filename)
	mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = mimeTypeFromFilename(filename)
	}
	if err := validateVoiceAsset(filename, mimeType, header.Size); kind == "voice" && err != nil {
		writeAssetValidationError(w, err)
		return
	}

	assetID := store.NewID()
	stored, err := s.assets.Save(r.Context(), AssetStorageSaveRequest{
		UserID:   user.ID,
		AssetID:  assetID,
		Filename: filename,
		MaxBytes: maxVoiceAssetBytes,
	}, file)
	if err != nil {
		writeAssetStorageError(w, err)
		return
	}

	asset, err := s.store.CreateAsset(domain.Asset{
		ID:         assetID,
		UserID:     user.ID,
		Kind:       kind,
		Filename:   filename,
		MimeType:   mimeType,
		Size:       stored.Size,
		StorageKey: stored.StorageKey,
		URL:        assetMetadataURL(assetID),
		ContentURL: assetContentURL(assetID),
		Checksum:   stored.Checksum,
	})
	if err != nil {
		_ = s.assets.Delete(r.Context(), &domain.Asset{StorageKey: stored.StorageKey})
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	asset = normalizeAssetURLs(asset)
	s.audit(r, user, "asset.create", "asset", asset.ID, map[string]string{"kind": asset.Kind, "mode": "upload"})
	writeOK(w, asset)
}
func (s *Server) serveAssetContent(w http.ResponseWriter, r *http.Request, asset *domain.Asset) {
	reader, err := s.assets.Open(r.Context(), asset)
	if err != nil {
		writeAssetStorageError(w, err)
		return
	}
	defer reader.Close()
	if asset.MimeType != "" {
		w.Header().Set("Content-Type", asset.MimeType)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(asset.Filename)))
	http.ServeContent(w, r, filepath.Base(asset.Filename), asset.CreatedAt, reader)
}
