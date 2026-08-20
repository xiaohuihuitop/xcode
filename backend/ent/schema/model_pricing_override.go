package schema

import (
	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ModelPricingOverride is the administrator-owned price catalog. It is
// intentionally keyed by adapter and model pattern, never by Group/Channel.
type ModelPricingOverride struct {
	ent.Schema
}

func (ModelPricingOverride) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "model_pricing_overrides"}}
}

func (ModelPricingOverride) Fields() []ent.Field {
	return []ent.Field{
		field.String("adapter").MaxLen(50).NotEmpty(),
		field.String("model_pattern").MaxLen(100).NotEmpty(),
		field.String("billing_mode").MaxLen(20).Default("token"),
		field.Float("input_price").Optional().Nillable(),
		field.Float("output_price").Optional().Nillable(),
		field.Float("cache_write_price").Optional().Nillable(),
		field.Float("cache_read_price").Optional().Nillable(),
		field.Float("image_input_price").Optional().Nillable(),
		field.Float("image_output_price").Optional().Nillable(),
		field.Float("per_request_price").Optional().Nillable(),
		field.JSON("intervals", []domain.ModelPricingInterval{}).
			Default([]domain.ModelPricingInterval{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("status").MaxLen(20).Default("active"),
	}
}

func (ModelPricingOverride) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("adapter", "model_pattern").Unique(),
		index.Fields("adapter", "status"),
	}
}
