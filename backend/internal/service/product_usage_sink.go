package service

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/applicationgateway"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
)

var ErrProductUsageFinalizerUnavailable = errors.New("product usage finalizer is unavailable")

type ProductUsageRecord struct {
	Snapshot applicationgateway.DecisionSnapshot
	Event    gatewayruntime.UsageEvent
}

type ProductUsageFinalizer interface {
	Finalize(context.Context, ProductUsageRecord) error
}

type ProductUsageSinkFactory struct {
	finalizer ProductUsageFinalizer
}

func NewProductUsageSinkFactory(finalizer ProductUsageFinalizer) *ProductUsageSinkFactory {
	return &ProductUsageSinkFactory{finalizer: finalizer}
}

func (f *ProductUsageSinkFactory) ForDecision(snapshot applicationgateway.DecisionSnapshot) gatewayruntime.UsageSink {
	if f == nil || f.finalizer == nil {
		return nil
	}
	return &productUsageSink{snapshot: snapshot, finalizer: f.finalizer}
}

type productUsageSink struct {
	snapshot  applicationgateway.DecisionSnapshot
	finalizer ProductUsageFinalizer
}

func (s *productUsageSink) RecordFinal(ctx context.Context, event gatewayruntime.UsageEvent) error {
	if s == nil || s.finalizer == nil {
		return ErrProductUsageFinalizerUnavailable
	}
	return s.finalizer.Finalize(ctx, ProductUsageRecord{Snapshot: s.snapshot, Event: event})
}
