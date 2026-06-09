package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AssetsListPage is the parsed, redacted result of one Cloud Asset Inventory
// assets.list response page. Resources carry only safe control-plane metadata;
// the raw provider resource data blob (which can embed network IPs, startup
// scripts, secrets, and other data-plane content) is dropped at parse time.
type AssetsListPage struct {
	// ReadTime is the response read time.
	ReadTime time.Time
	// Resources holds the safe observations parsed from the page.
	Resources []ResourceObservation
	// NextPageToken is the continuation token for the next page, empty when the
	// shard is complete.
	NextPageToken string
}

// SearchAllResourcesPage is the parsed, redacted result of one Cloud Asset
// Inventory searchAllResources response page.
type SearchAllResourcesPage = AssetsListPage

// ParseAssetsListPage parses one assets.list response page into safe resource
// observations. Only the full resource name, asset type, location, labels,
// ancestors, state, and update time are kept. The raw resource data blob is
// never carried into the observation.
func ParseAssetsListPage(raw []byte) (AssetsListPage, error) {
	var wire struct {
		ReadTime      string `json:"readTime"`
		NextPageToken string `json:"nextPageToken"`
		Assets        []struct {
			Name       string   `json:"name"`
			AssetType  string   `json:"assetType"`
			UpdateTime string   `json:"updateTime"`
			Ancestors  []string `json:"ancestors"`
			Resource   struct {
				Location string `json:"location"`
				Data     struct {
					Name        string            `json:"name"`
					DisplayName string            `json:"displayName"`
					Status      string            `json:"status"`
					State       string            `json:"state"`
					Labels      map[string]string `json:"labels"`
				} `json:"data"`
			} `json:"resource"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return AssetsListPage{}, fmt.Errorf("parse assets.list page: %w", err)
	}

	page := AssetsListPage{
		ReadTime:      parseTime(wire.ReadTime),
		NextPageToken: strings.TrimSpace(wire.NextPageToken),
		Resources:     make([]ResourceObservation, 0, len(wire.Assets)),
	}
	for _, asset := range wire.Assets {
		display := firstNonEmpty(asset.Resource.Data.DisplayName, asset.Resource.Data.Name)
		state := firstNonEmpty(asset.Resource.Data.State, asset.Resource.Data.Status)
		page.Resources = append(page.Resources, ResourceObservation{
			Name:        strings.TrimSpace(asset.Name),
			AssetType:   strings.TrimSpace(asset.AssetType),
			DisplayName: display,
			State:       state,
			Location:    strings.TrimSpace(asset.Resource.Location),
			Ancestors:   cloneStrings(asset.Ancestors),
			Labels:      cloneStringMap(asset.Resource.Data.Labels),
			UpdateTime:  parseTime(asset.UpdateTime),
		})
	}
	return page, nil
}

// ParseSearchAllResourcesPage parses one searchAllResources response page into
// safe resource observations. As with ParseAssetsListPage, only bounded
// control-plane metadata is retained; the additionalAttributes blob is dropped
// because it can carry network ranges and other data-plane-adjacent content not
// in scope for the first slice.
func ParseSearchAllResourcesPage(raw []byte) (SearchAllResourcesPage, error) {
	var wire struct {
		ReadTime      string `json:"readTime"`
		NextPageToken string `json:"nextPageToken"`
		Results       []struct {
			Name         string            `json:"name"`
			AssetType    string            `json:"assetType"`
			DisplayName  string            `json:"displayName"`
			Location     string            `json:"location"`
			State        string            `json:"state"`
			UpdateTime   string            `json:"updateTime"`
			Labels       map[string]string `json:"labels"`
			Folders      []string          `json:"folders"`
			Organization string            `json:"organization"`
			Project      string            `json:"project"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return SearchAllResourcesPage{}, fmt.Errorf("parse searchAllResources page: %w", err)
	}

	page := SearchAllResourcesPage{
		ReadTime:      parseTime(wire.ReadTime),
		NextPageToken: strings.TrimSpace(wire.NextPageToken),
		Resources:     make([]ResourceObservation, 0, len(wire.Results)),
	}
	for _, result := range wire.Results {
		page.Resources = append(page.Resources, ResourceObservation{
			Name:        strings.TrimSpace(result.Name),
			AssetType:   strings.TrimSpace(result.AssetType),
			DisplayName: strings.TrimSpace(result.DisplayName),
			State:       strings.TrimSpace(result.State),
			Location:    strings.TrimSpace(result.Location),
			Ancestors:   searchAncestors(result.Project, result.Folders, result.Organization),
			Labels:      cloneStringMap(result.Labels),
			UpdateTime:  parseTime(result.UpdateTime),
		})
	}
	return page, nil
}

// searchAncestors assembles an ordered ancestor chain from the discrete project,
// folder, and organization fields a searchAllResources result carries, matching
// the most-specific-first ordering assets.list uses in its ancestors array.
func searchAncestors(project string, folders []string, organization string) []string {
	ancestors := make([]string, 0, len(folders)+2)
	if proj := strings.TrimSpace(project); proj != "" {
		ancestors = append(ancestors, proj)
	}
	ancestors = append(ancestors, cloneStrings(folders)...)
	if org := strings.TrimSpace(organization); org != "" {
		ancestors = append(ancestors, org)
	}
	if len(ancestors) == 0 {
		return nil
	}
	return ancestors
}

func parseTime(value string) time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}
