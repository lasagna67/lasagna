package domain

import "context"

type claimsKey struct{}

// WithClaims кладёт данные пользователя в контекст. Вызывается middleware Authenticate.
func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

// GetClaims достаёт id и роль пользователя из контекста запроса.
// ok == false — запрос не прошёл через middleware Authenticate.
func GetClaims(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(Claims)
	return claims, ok
}
