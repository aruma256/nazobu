package auth

import "context"

type userContextKey struct{}

// WithUser は認証済みユーザーを context に注入する。
func WithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, userContextKey{}, user)
}

// UserFromContext は WithUser で注入されたユーザーを取り出す。無ければ nil。
func UserFromContext(ctx context.Context) *User {
	v, _ := ctx.Value(userContextKey{}).(*User)
	return v
}
