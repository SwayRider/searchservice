package server

import (
	"context"

	swjwt "github.com/swayrider/swlib/jwt"
	"github.com/swayrider/swlib/security"
	"google.golang.org/grpc/metadata"
)

// resolveUserID returns the user ID for the request.
// When called via the gateway (service token), it reads the forwarded x-user-id metadata.
// When called directly with a user JWT, it reads claims.Subject.
func resolveUserID(ctx context.Context) string {
	claims, ok := security.GetClaims(ctx)
	if !ok || claims == nil {
		return ""
	}
	if _, isService := claims.SwayRiderClaims.(*swjwt.SwayRiderServiceClaims); isService {
		md, ok := metadata.FromIncomingContext(ctx)
		if ok {
			if vals := md.Get("x-user-id"); len(vals) > 0 {
				return vals[0]
			}
		}
		return ""
	}
	return claims.Subject
}

// resolveAccountLevel returns the account level for the request.
// When called via the gateway (service token), it reads the forwarded x-account-level metadata.
// When called directly with a user JWT, it reads the user claims.
func resolveAccountLevel(ctx context.Context) string {
	claims, ok := security.GetClaims(ctx)
	if !ok || claims == nil {
		return "standard"
	}
	if userClaims, ok := claims.SwayRiderClaims.(*swjwt.SwayRiderUserClaims); ok {
		return userClaims.AccountLevel
	}
	md, _ := metadata.FromIncomingContext(ctx)
	if vals := md.Get("x-account-level"); len(vals) > 0 {
		return vals[0]
	}
	return "standard"
}
