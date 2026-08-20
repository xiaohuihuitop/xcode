package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Platform owns one provider-specific account pool.
type Platform struct {
	ent.Schema
}

func (Platform) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "platforms"},
	}
}

func (Platform) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (Platform) Fields() []ent.Field {
	return []ent.Field{
		field.String("code").MaxLen(50).NotEmpty().Unique(),
		field.String("name").MaxLen(100).NotEmpty(),
		// The adapter used by accounts in this pool. A platform code identifies
		// a business pool, so GPT and GLM can both use the OpenAI adapter.
		field.String("account_platform").MaxLen(50).NotEmpty(),
		field.String("status").MaxLen(20).Default(domain.StatusActive),
		field.JSON("endpoint_capabilities", []string{}).
			Default([]string{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
	}
}

func (Platform) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("model_rules", PlatformModelRule.Type),
		edge.To("accounts", Account.Type),
		edge.From("api_keys", APIKey.Type).Ref("platforms"),
		edge.To("usage_logs", UsageLog.Type),
	}
}

func (Platform) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
	}
}
