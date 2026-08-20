package service

import (
	"context"
	"sort"
	"strings"
)

// ListAuthorizedModels returns the concrete public model IDs exposed by the
// platforms assigned to an API key. Wildcard rules are routing rules, not
// concrete catalog entries, so they are intentionally omitted from /v1/models.
func (s *PlatformService) ListAuthorizedModels(ctx context.Context, platformIDs []int64) ([]string, error) {
	if len(platformIDs) == 0 {
		return nil, ErrAPIKeyPlatformForbidden
	}
	allowed := make(map[int64]struct{}, len(platformIDs))
	for _, platformID := range platformIDs {
		if platformID > 0 {
			allowed[platformID] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil, ErrAPIKeyPlatformForbidden
	}

	rules, err := s.repo.ListModelRules(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	models := make([]string, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if _, ok := allowed[rule.PlatformID]; !ok {
			continue
		}
		model := strings.TrimSpace(rule.ModelPattern)
		if model == "" || strings.Contains(model, "*") {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	sort.Strings(models)
	return models, nil
}
