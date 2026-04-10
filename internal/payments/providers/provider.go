package providers

import "context"

type DynamicQRProvider interface {
	CreateDynamicQR(ctx context.Context, input any) (any, error)
	GetPaymentStatus(ctx context.Context, input any) (any, error)
	VerifyWebhook(ctx context.Context, input any) error
	ParseWebhook(ctx context.Context, input any) (any, error)
}
