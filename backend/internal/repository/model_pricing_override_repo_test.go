//go:build unit

package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestModelPricingOverrideRepositoryUpsertUsesExpressionConflictKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := NewModelPricingOverrideRepository(db)
	zero := 0.0
	override := &service.ModelPricingOverride{
		Adapter: " Codex ", ModelPattern: "gpt-5.6-sol", BillingMode: service.BillingModeToken,
		InputPrice: &zero, Status: service.ModelPricingStatusActive,
	}
	mock.ExpectQuery(regexp.QuoteMeta("ON CONFLICT ((LOWER(adapter)), model_pattern) DO UPDATE SET")).
		WithArgs("codex", "gpt-5.6-sol", service.BillingModeToken, &zero, nil, nil, nil, nil, nil, nil, []byte("[]"), service.ModelPricingStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))

	err = repo.Upsert(context.Background(), override)

	require.NoError(t, err)
	require.Equal(t, int64(42), override.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}
