package httpapi

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
	"github.com/eqzhou/flyaimovie/internal/services/mediainfo"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxMediaUploadBytes int64 = 500 << 20

func (s *Server) uploadMedia(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader.Size < 1 || fileHeader.Size > maxMediaUploadBytes {
		response.BadRequest(c, "media file is required and must not exceed 500 MB")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.BadRequest(c, "failed to open media file")
		return
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	header, _ := reader.Peek(512)
	mime := http.DetectContentType(header)
	assetType := ""
	switch {
	case strings.HasPrefix(mime, "video/") || mime == "application/octet-stream":
		assetType = "video"
	case strings.HasPrefix(mime, "audio/"):
		assetType = "audio"
	default:
		response.BadRequest(c, "only video and audio uploads are supported")
		return
	}
	dramaID := optionalFormID(c.PostForm("drama_id"))
	episodeID := optionalFormID(c.PostForm("episode_id"))
	storyboardID := optionalFormID(c.PostForm("storyboard_id"))
	if err := validateAssetOwnership(c, dramaID, episodeID, storyboardID); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	hash := sha256.New()
	rel, abs, err := s.Store.Save("uploads", fileHeader.Filename, io.TeeReader(io.LimitReader(reader, maxMediaUploadBytes+1), hash))
	if err != nil {
		response.ServerError(c, "failed to save media")
		return
	}
	now := response.Now()
	asset := models.Asset{OrganizationID: currentOrganizationID(c), DramaID: dramaID, EpisodeID: episodeID, StoryboardID: storyboardID,
		Name: firstNonEmpty(strings.TrimSpace(c.PostForm("name")), filepath.Base(fileHeader.Filename)), Type: assetType,
		Category: firstNonEmpty(strings.TrimSpace(c.PostForm("category")), "reference"), URL: s.Store.PublicURL(rel),
		LocalPath: rel, FileSize: fileHeader.Size, MimeType: mime, ContentHash: hex.EncodeToString(hash.Sum(nil)),
		ReferenceCount: 1, ProbeStatus: "pending", CreatedAt: now, UpdatedAt: now}
	applyProbe(c.Request.Context(), abs, &asset)
	var cacheObject *models.MediaCacheObject
	reused := false
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&asset).Error; err != nil {
			return err
		}
		var cacheErr error
		cacheObject, reused, cacheErr = mediacache.New(tx, s.Store).Put(mediacache.PutInput{OrganizationID: asset.OrganizationID, Namespace: "asset", Key: strconv.FormatUint(uint64(asset.ID), 10),
			ContentHash: asset.ContentHash, Kind: asset.Type, LocalPath: asset.LocalPath, PublicURL: asset.URL, MimeType: asset.MimeType, Size: asset.FileSize})
		if cacheErr != nil {
			return cacheErr
		}
		if reused && cacheObject.LocalPath != asset.LocalPath {
			asset.LocalPath, asset.URL = cacheObject.LocalPath, cacheObject.PublicURL
			return tx.Model(&asset).Updates(map[string]any{"local_path": asset.LocalPath, "url": asset.URL, "updated_at": response.Now()}).Error
		}
		return nil
	})
	if err != nil {
		_ = os.Remove(abs)
		response.ServerError(c, "failed to register media")
		return
	}
	if reused && cacheObject != nil && cacheObject.LocalPath != rel {
		_ = os.Remove(abs)
	}
	response.Created(c, asset)
}

func optionalFormID(value string) *uint {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil || parsed == 0 {
		return nil
	}
	id := uint(parsed)
	return &id
}

func applyProbe(ctx context.Context, path string, asset *models.Asset) {
	info, err := mediainfo.Probe(ctx, path)
	if err != nil {
		asset.ProbeStatus, asset.ProbeError = "failed", err.Error()
		return
	}
	asset.ProbeStatus, asset.ProbeError = "completed", ""
	asset.DurationSeconds, asset.FrameRate, asset.Codec, asset.Format = info.Duration, info.FrameRate, info.Codec, info.Format
	asset.Width, asset.Height = info.Width, info.Height
	asset.Duration = int(info.Duration + 0.5)
}

func (s *Server) probeAsset(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "invalid asset id")
		return
	}
	var asset models.Asset
	if err := organizationDB(c).Where("id = ? AND deleted_at IS NULL", id).First(&asset).Error; err != nil {
		response.NotFound(c, "asset not found")
		return
	}
	path, err := s.Store.Resolve(asset.LocalPath)
	if err != nil {
		response.BadRequest(c, "asset has no readable local file")
		return
	}
	applyProbe(c.Request.Context(), path, &asset)
	if err := organizationDB(c).Model(&asset).Updates(map[string]any{"duration": asset.Duration, "duration_seconds": asset.DurationSeconds,
		"width": asset.Width, "height": asset.Height, "frame_rate": asset.FrameRate, "codec": asset.Codec, "format": asset.Format,
		"probe_status": asset.ProbeStatus, "probe_error": asset.ProbeError, "updated_at": response.Now()}).Error; err != nil {
		response.ServerError(c, "failed to save media metadata")
		return
	}
	if asset.ProbeStatus == "failed" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": http.StatusUnprocessableEntity, "message": asset.ProbeError, "data": asset})
		return
	}
	response.Success(c, asset)
}

func (s *Server) repairAssetMetadata(c *gin.Context) {
	var rows []models.Asset
	if err := organizationDB(c).Where("deleted_at IS NULL AND local_path <> '' AND type IN ?", []string{"video", "audio"}).Limit(500).Find(&rows).Error; err != nil {
		response.ServerError(c, "failed to list media")
		return
	}
	succeeded, failed := 0, 0
	for index := range rows {
		path, err := s.Store.Resolve(rows[index].LocalPath)
		if err != nil {
			failed++
			continue
		}
		applyProbe(c.Request.Context(), path, &rows[index])
		if rows[index].ProbeStatus == "completed" {
			succeeded++
		} else {
			failed++
		}
		organizationDB(c).Model(&rows[index]).Updates(map[string]any{"duration": rows[index].Duration, "duration_seconds": rows[index].DurationSeconds,
			"width": rows[index].Width, "height": rows[index].Height, "frame_rate": rows[index].FrameRate, "codec": rows[index].Codec,
			"format": rows[index].Format, "probe_status": rows[index].ProbeStatus, "probe_error": rows[index].ProbeError, "updated_at": response.Now()})
	}
	response.Success(c, gin.H{"total": len(rows), "succeeded": succeeded, "failed": failed})
}
