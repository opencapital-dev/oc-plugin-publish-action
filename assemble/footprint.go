package main

import (
	"encoding/json"
	"errors"
	"fmt"
)

// LogicalView mirrors install.LogicalView.
type LogicalView struct {
	Name        string `json:"name"`
	SourceTable string `json:"source_table"`
}

// QueryEntity mirrors install.QueryEntity.
type QueryEntity struct {
	Name       string            `json:"name"`
	Table      string            `json:"table"`
	Scope      string            `json:"scope"`
	ScopeLabel string            `json:"scope_label"`
	TimeCol    string            `json:"time_col"`
	Grain      []string          `json:"grain"`
	Labels     map[string]string `json:"labels"`
	Columns    []string          `json:"columns"`
	Kinds      map[string]string `json:"kinds"`
}

// Footprint mirrors install.Footprint — the JSON the control-plane unmarshals
// from the OCI config blob.
type Footprint struct {
	PluginID       string        `json:"plugin_id"`
	GrafanaSlug    string        `json:"grafana_slug"`
	Type           string        `json:"type"`
	DisplayName    string        `json:"display_name"`
	Description    string        `json:"description"`
	PlatformPlugin bool          `json:"platform_plugin"`
	LogicalViews   []LogicalView `json:"logical_views"`
	QueryEntities  []QueryEntity `json:"query_entities"`
}

type pluginJSON struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
	Info struct {
		Description string `json:"description"`
	} `json:"info"`
	OpenCapital struct {
		PluginID       string        `json:"plugin_id"`
		PlatformPlugin bool          `json:"platform_plugin"`
		LogicalViews   []LogicalView `json:"logical_views"`
		QueryEntities  []QueryEntity `json:"query_entities"`
	} `json:"opencapital"`
}

func footprintFromPluginJSON(b []byte) (Footprint, error) {
	var pj pluginJSON
	if err := json.Unmarshal(b, &pj); err != nil {
		return Footprint{}, fmt.Errorf("parse plugin.json: %w", err)
	}

	switch pj.Type {
	case "app", "datasource", "panel":
	case "":
		return Footprint{}, errors.New("plugin.json type is empty: must be one of app, datasource, panel")
	default:
		return Footprint{}, fmt.Errorf("plugin.json type %q is not valid: must be one of app, datasource, panel", pj.Type)
	}

	if pj.OpenCapital.PluginID == "" {
		return Footprint{}, errors.New("plugin.json opencapital.plugin_id is empty: the logical control-plane id is required")
	}

	return Footprint{
		PluginID:       pj.OpenCapital.PluginID,
		GrafanaSlug:    pj.ID,
		Type:           pj.Type,
		DisplayName:    pj.Name,
		Description:    pj.Info.Description,
		PlatformPlugin: pj.OpenCapital.PlatformPlugin,
		LogicalViews:   pj.OpenCapital.LogicalViews,
		QueryEntities:  pj.OpenCapital.QueryEntities,
	}, nil
}
