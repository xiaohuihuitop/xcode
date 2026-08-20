package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PlatformModelRule maps a client model pattern to one enabled platform.
type PlatformModelRule struct {
	ent.Schema
}

func (PlatformModelRule) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "platform_model_rules"},
	}
}

func (PlatformModelRule) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (PlatformModelRule) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("platform_id"),
		field.String("model_pattern").MaxLen(100).NotEmpty(),
		field.String("upstream_model").MaxLen(100).Default(""),
		field.String("status").MaxLen(20).Default(domain.StatusActive),
	}
}

func (PlatformModelRule) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("platform", Platform.Type).Ref("model_rules").Field("platform_id").Unique().Required(),
	}
}

func (PlatformModelRule) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("platform_id", "model_pattern").Unique(),
		index.Fields("status"),
	}
}
