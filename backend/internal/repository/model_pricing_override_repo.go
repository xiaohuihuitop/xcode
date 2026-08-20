package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type modelPricingOverrideRepository struct {
	db *sql.DB
}

func NewModelPricingOverrideRepository(db *sql.DB) service.ModelPricingOverrideRepository {
	return &modelPricingOverrideRepository{db: db}
}

func (r *modelPricingOverrideRepository) List(ctx context.Context, adapter string) ([]service.ModelPricingOverride, error) {
	query := `SELECT id, adapter, model_pattern, billing_mode,
        input_price, output_price, cache_write_price, cache_read_price,
        image_input_price, image_output_price, per_request_price, intervals, status
        FROM model_pricing_overrides`
	args := []any{}
	if strings.TrimSpace(adapter) != "" {
		query += " WHERE LOWER(adapter) = LOWER($1)"
		args = append(args, strings.TrimSpace(adapter))
	}
	query += " ORDER BY adapter ASC, model_pattern ASC, id ASC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list model pricing overrides: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]service.ModelPricingOverride, 0)
	for rows.Next() {
		item, err := scanModelPricingOverride(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model pricing overrides: %w", err)
	}
	return result, nil
}

func (r *modelPricingOverrideRepository) Get(ctx context.Context, id int64) (*service.ModelPricingOverride, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, adapter, model_pattern, billing_mode,
        input_price, output_price, cache_write_price, cache_read_price,
        image_input_price, image_output_price, per_request_price, intervals, status
        FROM model_pricing_overrides WHERE id = $1`, id)
	item, err := scanModelPricingOverride(row)
	if err == sql.ErrNoRows {
		return nil, service.ErrModelPricingOverrideNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get model pricing override: %w", err)
	}
	return &item, nil
}

func (r *modelPricingOverrideRepository) Create(ctx context.Context, override *service.ModelPricingOverride) error {
	if override == nil {
		return fmt.Errorf("model pricing override is nil")
	}
	intervals, err := json.Marshal(normalizeIntervals(override.Intervals))
	if err != nil {
		return fmt.Errorf("marshal model pricing intervals: %w", err)
	}
	err = r.db.QueryRowContext(ctx, `INSERT INTO model_pricing_overrides
        (adapter, model_pattern, billing_mode, input_price, output_price,
         cache_write_price, cache_read_price, image_input_price,
         image_output_price, per_request_price, intervals, status)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
        RETURNING id`, strings.ToLower(strings.TrimSpace(override.Adapter)),
		strings.TrimSpace(override.ModelPattern), normalizeBillingMode(override.BillingMode),
		override.InputPrice, override.OutputPrice, override.CacheWritePrice,
		override.CacheReadPrice, override.ImageInputPrice, override.ImageOutputPrice,
		override.PerRequestPrice, intervals, normalizeStatus(override.Status)).Scan(&override.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return service.ErrModelPricingOverrideConflict
		}
		return fmt.Errorf("create model pricing override: %w", err)
	}
	return nil
}

func (r *modelPricingOverrideRepository) Update(ctx context.Context, override *service.ModelPricingOverride) error {
	if override == nil || override.ID <= 0 {
		return service.ErrModelPricingOverrideNotFound
	}
	intervals, err := json.Marshal(normalizeIntervals(override.Intervals))
	if err != nil {
		return fmt.Errorf("marshal model pricing intervals: %w", err)
	}
	result, err := r.db.ExecContext(ctx, `UPDATE model_pricing_overrides SET
        adapter = $1, model_pattern = $2, billing_mode = $3, input_price = $4,
        output_price = $5, cache_write_price = $6, cache_read_price = $7,
        image_input_price = $8, image_output_price = $9, per_request_price = $10,
        intervals = $11, status = $12, updated_at = NOW() WHERE id = $13`,
		strings.ToLower(strings.TrimSpace(override.Adapter)), strings.TrimSpace(override.ModelPattern),
		normalizeBillingMode(override.BillingMode), override.InputPrice, override.OutputPrice,
		override.CacheWritePrice, override.CacheReadPrice, override.ImageInputPrice,
		override.ImageOutputPrice, override.PerRequestPrice, intervals,
		normalizeStatus(override.Status), override.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return service.ErrModelPricingOverrideConflict
		}
		return fmt.Errorf("update model pricing override: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read model pricing override update result: %w", err)
	}
	if rows == 0 {
		return service.ErrModelPricingOverrideNotFound
	}
	return nil
}

func (r *modelPricingOverrideRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM model_pricing_overrides WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete model pricing override: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read model pricing override delete result: %w", err)
	}
	if rows == 0 {
		return service.ErrModelPricingOverrideNotFound
	}
	return nil
}

type modelPricingRowScanner interface {
	Scan(dest ...any) error
}

func scanModelPricingOverride(row modelPricingRowScanner) (service.ModelPricingOverride, error) {
	var item service.ModelPricingOverride
	var mode, status string
	var inputPrice, outputPrice, cacheWritePrice, cacheReadPrice sql.NullFloat64
	var imageInputPrice, imageOutputPrice, perRequestPrice sql.NullFloat64
	var rawIntervals []byte
	if err := row.Scan(&item.ID, &item.Adapter, &item.ModelPattern, &mode,
		&inputPrice, &outputPrice, &cacheWritePrice, &cacheReadPrice,
		&imageInputPrice, &imageOutputPrice, &perRequestPrice, &rawIntervals, &status); err != nil {
		return service.ModelPricingOverride{}, fmt.Errorf("scan model pricing override: %w", err)
	}
	item.BillingMode = service.BillingMode(mode)
	item.Status = status
	item.InputPrice = nullableFloat(inputPrice)
	item.OutputPrice = nullableFloat(outputPrice)
	item.CacheWritePrice = nullableFloat(cacheWritePrice)
	item.CacheReadPrice = nullableFloat(cacheReadPrice)
	item.ImageInputPrice = nullableFloat(imageInputPrice)
	item.ImageOutputPrice = nullableFloat(imageOutputPrice)
	item.PerRequestPrice = nullableFloat(perRequestPrice)
	if len(rawIntervals) > 0 {
		if err := json.Unmarshal(rawIntervals, &item.Intervals); err != nil {
			return service.ModelPricingOverride{}, fmt.Errorf("decode model pricing intervals: %w", err)
		}
	}
	item.Intervals = normalizeIntervals(item.Intervals)
	return item, nil
}

func nullableFloat(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	result := value.Float64
	return &result
}

func normalizeIntervals(intervals []domain.ModelPricingInterval) []domain.ModelPricingInterval {
	if intervals == nil {
		return []domain.ModelPricingInterval{}
	}
	return intervals
}

func normalizeBillingMode(mode service.BillingMode) service.BillingMode {
	if mode == "" {
		return service.BillingModeToken
	}
	return mode
}

func normalizeStatus(status string) string {
	if strings.TrimSpace(status) == "" {
		return service.ModelPricingStatusActive
	}
	return strings.ToLower(strings.TrimSpace(status))
}
