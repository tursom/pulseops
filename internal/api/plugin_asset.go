package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	pluginmgr "pulseops/internal/plugin"
)

const maxPluginAssetBytes = 128 << 20

type pluginAssetUpload struct {
	Filename    string
	ContentType string
	Reader      io.Reader
	close       func() error
}

func (u pluginAssetUpload) Close() error {
	if u.close == nil {
		return nil
	}
	return u.close()
}

func buildPluginAsset(ctx context.Context, pluginManager PluginManager, configStore PluginConfigStore, req pluginAssetRequest) (pluginmgr.AssetRecord, error) {
	id := strings.TrimSpace(req.ID)
	pluginID := strings.TrimSpace(req.PluginID)
	capabilityID := strings.TrimSpace(req.CapabilityID)
	configInstanceID := strings.TrimSpace(req.ConfigInstanceID)
	scope := strings.TrimSpace(req.Scope)
	kind := strings.TrimSpace(req.Kind)
	title := strings.TrimSpace(req.Title)
	if id == "" {
		return pluginmgr.AssetRecord{}, errors.New("id is required")
	}
	if pluginID == "" {
		return pluginmgr.AssetRecord{}, errors.New("plugin_id is required")
	}
	if kind == "" {
		return pluginmgr.AssetRecord{}, errors.New("kind is required")
	}
	if scope == "" {
		return pluginmgr.AssetRecord{}, errors.New("scope is required")
	}
	item, err := pluginManager.Plugin(ctx, pluginID)
	if err != nil {
		return pluginmgr.AssetRecord{}, fmt.Errorf("plugin %s not found: %w", pluginID, err)
	}
	switch scope {
	case pluginmgr.AssetScopePluginShared:
		if capabilityID != "" || configInstanceID != "" {
			return pluginmgr.AssetRecord{}, errors.New("plugin_shared assets must not set capability_id or config_instance_id")
		}
	case pluginmgr.AssetScopeCapabilityShared:
		if capabilityID == "" {
			return pluginmgr.AssetRecord{}, errors.New("capability_shared assets require capability_id")
		}
		capability, err := findPluginCapability(ctx, pluginManager, capabilityID)
		if err != nil {
			return pluginmgr.AssetRecord{}, err
		}
		if capability.PluginID != pluginID {
			return pluginmgr.AssetRecord{}, fmt.Errorf("capability %s belongs to plugin %s", capabilityID, capability.PluginID)
		}
		if configInstanceID != "" {
			return pluginmgr.AssetRecord{}, errors.New("capability_shared assets must not set config_instance_id")
		}
	case pluginmgr.AssetScopeConfigInstance:
		if configInstanceID == "" {
			return pluginmgr.AssetRecord{}, errors.New("config_instance assets require config_instance_id")
		}
		instance, err := configStore.GetPluginConfigInstance(ctx, configInstanceID)
		if err != nil {
			return pluginmgr.AssetRecord{}, fmt.Errorf("plugin config instance %s not found: %w", configInstanceID, err)
		}
		if instance.PluginID != pluginID {
			return pluginmgr.AssetRecord{}, fmt.Errorf("plugin config instance %s belongs to plugin %s", configInstanceID, instance.PluginID)
		}
		if capabilityID != "" && capabilityID != instance.CapabilityID {
			return pluginmgr.AssetRecord{}, fmt.Errorf("plugin config instance %s belongs to capability %s", configInstanceID, instance.CapabilityID)
		}
		capabilityID = instance.CapabilityID
	default:
		return pluginmgr.AssetRecord{}, fmt.Errorf("scope %q is not supported", scope)
	}
	if title == "" {
		title = id
	}
	return pluginmgr.AssetRecord{
		ID:               id,
		PluginID:         item.Package.ID,
		CapabilityID:     capabilityID,
		ConfigInstanceID: configInstanceID,
		Scope:            scope,
		Kind:             kind,
		Title:            title,
		Status:           "draft",
	}, nil
}

func readPluginAssetUpload(r *http.Request) (pluginAssetUpload, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		file, header, err := r.FormFile("file")
		if err != nil {
			return pluginAssetUpload{}, fmt.Errorf("file is required")
		}
		filename := safePluginPathSegment(path.Base(strings.ReplaceAll(header.Filename, "\\", "/")))
		if filename == "" || filename == "." {
			_ = file.Close()
			return pluginAssetUpload{}, fmt.Errorf("filename is required")
		}
		partContentType := header.Header.Get("Content-Type")
		return pluginAssetUpload{
			Filename:    filename,
			ContentType: partContentType,
			Reader:      file,
			close:       file.Close,
		}, nil
	}
	filename := safePluginPathSegment(path.Base(strings.ReplaceAll(r.URL.Query().Get("filename"), "\\", "/")))
	if filename == "" || filename == "." {
		return pluginAssetUpload{}, fmt.Errorf("filename is required")
	}
	return pluginAssetUpload{
		Filename:    filename,
		ContentType: contentType,
		Reader:      r.Body,
		close:       r.Body.Close,
	}, nil
}

func pluginAssetObjectKey(asset pluginmgr.AssetRecord, version int, filename string) string {
	return path.Join(
		"plugins",
		safePluginPathSegment(asset.PluginID),
		"assets",
		safePluginPathSegment(asset.ID),
		strconv.Itoa(version),
		safePluginPathSegment(filename),
	)
}

func pluginAssetDBURI(asset pluginmgr.AssetRecord, version int, filename string) string {
	return "db://" + path.Join(
		"plugin-assets",
		safePluginPathSegment(asset.PluginID),
		safePluginPathSegment(asset.ID),
		strconv.Itoa(version),
		safePluginPathSegment(filename),
	)
}

func buildPluginAssetVersionRecord(asset pluginmgr.AssetRecord, version int, upload pluginAssetUpload) (pluginmgr.AssetVersionRecord, error) {
	raw, err := io.ReadAll(io.LimitReader(upload.Reader, maxPluginAssetBytes+1))
	if err != nil {
		return pluginmgr.AssetVersionRecord{}, fmt.Errorf("read asset file: %w", err)
	}
	if len(raw) > maxPluginAssetBytes {
		return pluginmgr.AssetVersionRecord{}, fmt.Errorf("asset file exceeds %d bytes", maxPluginAssetBytes)
	}
	sum := sha256.Sum256(raw)
	return pluginmgr.AssetVersionRecord{
		AssetID:     asset.ID,
		Version:     version,
		Status:      "draft",
		Filename:    upload.Filename,
		ContentType: upload.ContentType,
		StorageURI:  pluginAssetDBURI(asset, version, upload.Filename),
		Content:     raw,
		SizeBytes:   int64(len(raw)),
		Checksum:    "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}

func nextPluginAssetVersion(records []pluginmgr.AssetVersionRecord) int {
	next := 1
	for _, record := range records {
		if record.Version >= next {
			next = record.Version + 1
		}
	}
	return next
}

func validatePluginAssetVersion(record pluginmgr.AssetVersionRecord) error {
	if strings.TrimSpace(record.Filename) == "" {
		return errors.New("filename is required")
	}
	if strings.TrimSpace(record.StorageURI) == "" {
		return errors.New("storage_uri is required")
	}
	if strings.TrimSpace(record.Checksum) == "" {
		return errors.New("checksum is required")
	}
	if record.SizeBytes <= 0 {
		return errors.New("asset file must not be empty")
	}
	return nil
}

func safePluginPathSegment(input string) string {
	input = strings.TrimSpace(strings.ReplaceAll(input, "\\", "/"))
	if input == "" {
		return ""
	}
	input = strings.Trim(input, ".")
	if input == "" {
		return ""
	}
	replacer := strings.NewReplacer("/", "_", ":", "_", "@", "_", " ", "_")
	return replacer.Replace(input)
}
