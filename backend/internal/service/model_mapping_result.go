package service

import (
	"context"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ModelMappingResult records a transient model rewrite performed by an adapter.
type ModelMappingResult struct {
	MappedModel        string
	Mapped             bool
	BillingModelSource string
}

func (r ModelMappingResult) BuildModelMappingChain(requestedModel, upstreamModel string) string {
	if !r.Mapped {
		if upstreamModel != "" && upstreamModel != requestedModel {
			return requestedModel + "->" + upstreamModel
		}
		return ""
	}
	if upstreamModel != "" && upstreamModel != r.MappedModel {
		return requestedModel + "->" + r.MappedModel + "->" + upstreamModel
	}
	return requestedModel + "->" + r.MappedModel
}

func (r ModelMappingResult) ToUsageFields(requestedModel, upstreamModel string) ModelRoutingUsageFields {
	mappedModel := requestedModel
	if r.Mapped {
		mappedModel = r.MappedModel
	}
	return ModelRoutingUsageFields{
		OriginalModel:      requestedModel,
		MappedModel:        mappedModel,
		BillingModelSource: r.BillingModelSource,
		ModelMappingChain:  r.BuildModelMappingChain(requestedModel, upstreamModel),
	}
}

func platformAssetUpstreamModel(ctx context.Context, requestedModel string) (string, bool) {
	route, ok := GatewayPlatformAssetContextFromContext(ctx)
	if !ok || route.Platform == nil {
		return "", false
	}
	upstreamModel := strings.TrimSpace(route.Platform.UpstreamModel)
	return upstreamModel, upstreamModel != ""
}

func applyPlatformAssetModelMapping(result ModelMappingResult, upstreamModel string, hasPlatformRoute bool) ModelMappingResult {
	if !hasPlatformRoute {
		return result
	}
	result.Mapped = !strings.EqualFold(strings.TrimSpace(result.MappedModel), upstreamModel)
	result.MappedModel = upstreamModel
	result.BillingModelSource = BillingModelSourceMapped
	return result
}

func ReplaceModelInBody(body []byte, newModel string) []byte {
	if len(body) == 0 {
		return body
	}
	if current := gjson.GetBytes(body, "model"); current.Exists() && current.String() == newModel {
		return body
	}
	updated, err := sjson.SetBytes(body, "model", newModel)
	if err != nil {
		return body
	}
	return updated
}

func RemovePreviousResponseIDFromBody(body []byte) []byte {
	if len(body) == 0 || !gjson.GetBytes(body, "previous_response_id").Exists() {
		return body
	}
	updated, err := sjson.DeleteBytes(body, "previous_response_id")
	if err != nil {
		return body
	}
	return updated
}
