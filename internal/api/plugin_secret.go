package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pluginmgr "pulseops/internal/plugin"
	"pulseops/internal/pluginconfig"
)

type pluginSecretRequest struct {
	ID       string `json:"id"`
	PluginID string `json:"plugin_id"`
	Scope    string `json:"scope,omitempty"`
	Title    string `json:"title,omitempty"`
	Value    string `json:"value"`
}

func buildPluginSecret(ctx context.Context, pluginManager PluginManager, req pluginSecretRequest) (pluginmgr.SecretRecord, pluginmgr.SecretValueRecord, error) {
	id := strings.TrimSpace(req.ID)
	pluginID := strings.TrimSpace(req.PluginID)
	if id == "" {
		return pluginmgr.SecretRecord{}, pluginmgr.SecretValueRecord{}, errors.New("id is required")
	}
	if pluginID == "" {
		return pluginmgr.SecretRecord{}, pluginmgr.SecretValueRecord{}, errors.New("plugin_id is required")
	}
	if req.Value == "" {
		return pluginmgr.SecretRecord{}, pluginmgr.SecretValueRecord{}, errors.New("value is required")
	}
	item, err := pluginManager.Plugin(ctx, pluginID)
	if err != nil {
		return pluginmgr.SecretRecord{}, pluginmgr.SecretValueRecord{}, fmt.Errorf("plugin %s not found: %w", pluginID, err)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = id
	}
	ciphertext, meta, err := pluginconfig.EncryptSecret(req.Value)
	if err != nil {
		return pluginmgr.SecretRecord{}, pluginmgr.SecretValueRecord{}, err
	}
	now := time.Now().UTC()
	secret := pluginmgr.SecretRecord{
		ID:        id,
		PluginID:  item.Package.ID,
		Scope:     strings.TrimSpace(req.Scope),
		Title:     title,
		Masked:    pluginconfig.MaskSecret(req.Value),
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}
	value := pluginmgr.SecretValueRecord{
		SecretID:       id,
		Ciphertext:     ciphertext,
		EncryptionMeta: meta,
		UpdatedAt:      now,
	}
	return secret, value, nil
}
