package middleware

import (
	"context"

	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/auth"
	"github.com/BuzzLyutic/payment-gateway-microservices/services/api-gateway/internal/contextkeys"
)

func WithMerchant(ctx context.Context, info *auth.MerchantInfo) context.Context {
	return context.WithValue(ctx, contextkeys.MerchantInfo, info)
}

func MerchantFromContext(ctx context.Context) *auth.MerchantInfo {
	if v, ok := ctx.Value(contextkeys.MerchantInfo).(*auth.MerchantInfo); ok {
		return v
	}
	return nil
}
