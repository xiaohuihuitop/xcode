package service

import (
	"fmt"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrPlatformModelNotFound  = infraerrors.NotFound("PLATFORM_MODEL_NOT_FOUND", "model is not assigned to an active platform")
	ErrPlatformModelAmbiguous = infraerrors.Conflict("PLATFORM_MODEL_AMBIGUOUS", "model matches multiple platform rules")
	ErrPlatformModelRule      = infraerrors.BadRequest("INVALID_PLATFORM_MODEL_RULE", "invalid platform model rule")
)

type platformModelResolver struct {
	rules []PlatformModelRule
}

func newPlatformModelResolver(rules []PlatformModelRule) *platformModelResolver {
	return &platformModelResolver{rules: append([]PlatformModelRule(nil), rules...)}
}

func validatePlatformModelRules(rules []PlatformModelRule) error {
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		pattern, _, err := normalizePlatformModelPattern(rule.ModelPattern)
		if err != nil {
			return err
		}
		if rule.PlatformID <= 0 {
			return fmt.Errorf("%w: platform id is required", ErrPlatformModelRule)
		}
		key := fmt.Sprintf("%d:%s", rule.PlatformID, pattern)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate pattern %q on platform %d", ErrPlatformModelRule, rule.ModelPattern, rule.PlatformID)
		}
		seen[key] = struct{}{}
	}

	return nil
}

func (r *platformModelResolver) Resolve(requestedModel string) (*ResolvedPlatformModel, error) {
	candidates, err := r.ListCandidates(requestedModel)
	if err != nil {
		return nil, err
	}
	if len(candidates) > 1 {
		bestPriority := candidates[0].MatchPriority
		bestPlatformID := candidates[0].PlatformID
		for _, candidate := range candidates[1:] {
			if candidate.MatchPriority != bestPriority {
				break
			}
			if candidate.PlatformID != bestPlatformID {
				return nil, ErrPlatformModelAmbiguous
			}
		}
	}
	return candidates[0], nil
}

func (r *platformModelResolver) ListCandidates(requestedModel string) ([]*ResolvedPlatformModel, error) {
	requested := strings.TrimSpace(requestedModel)
	if requested == "" {
		return nil, ErrPlatformModelNotFound
	}

	candidates := make([]*ResolvedPlatformModel, 0)
	for index := range r.rules {
		rule := &r.rules[index]
		if !rule.Enabled {
			continue
		}
		pattern, wildcard, err := normalizePlatformModelPattern(rule.ModelPattern)
		if err != nil || !platformModelRuleMatches(strings.ToLower(requested), pattern, wildcard) {
			continue
		}
		upstream := rule.UpstreamModel
		if upstream == "" {
			upstream = requested
		}
		priority := len(strings.TrimSuffix(pattern, "*"))
		if !wildcard {
			priority += 1_000_000
		}
		candidates = append(candidates, &ResolvedPlatformModel{
			PlatformID:           rule.PlatformID,
			PlatformCode:         rule.PlatformCode,
			AccountPlatform:      rule.AccountPlatform,
			RequestedModel:       requested,
			UpstreamModel:        upstream,
			EndpointCapabilities: append([]string(nil), rule.EndpointCapabilities...),
			MatchPriority:        priority,
			RuleID:               rule.ID,
		})
	}
	if len(candidates) == 0 {
		return nil, ErrPlatformModelNotFound
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].MatchPriority != candidates[right].MatchPriority {
			return candidates[left].MatchPriority > candidates[right].MatchPriority
		}
		return candidates[left].RuleID < candidates[right].RuleID
	})
	return candidates, nil
}

func normalizeEndpointCapabilities(capabilities []string) []string {
	seen := make(map[string]struct{}, len(capabilities))
	result := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability == "" {
			continue
		}
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		result = append(result, capability)
	}
	sort.Strings(result)
	return result
}

func normalizePlatformModelPattern(raw string) (string, bool, error) {
	pattern := strings.ToLower(strings.TrimSpace(raw))
	if pattern == "" || strings.Count(pattern, "*") > 1 || (strings.Contains(pattern, "*") && !strings.HasSuffix(pattern, "*")) {
		return "", false, fmt.Errorf("%w: model pattern %q must be exact or use one suffix wildcard", ErrPlatformModelRule, raw)
	}
	return pattern, strings.HasSuffix(pattern, "*"), nil
}

func platformModelRuleMatches(requested, pattern string, wildcard bool) bool {
	if wildcard {
		return strings.HasPrefix(requested, strings.TrimSuffix(pattern, "*"))
	}
	return requested == pattern
}
