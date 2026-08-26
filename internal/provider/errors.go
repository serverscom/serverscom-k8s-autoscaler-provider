package provider

import (
	"context"

	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// toGRPCError maps an API error onto a gRPC status, preserving the API error code in the
// message.
//
// Nothing is swallowed and nothing is turned into a successful response: a conflict - the group
// is at its maximum or minimum, the target size would drop below the nodes the group already
// has, a node has not joined the cluster yet, the account's SBM limits are exceeded - has to
// reach the autoscaler as an error so that it can decide what to do next.
//
// The context is inspected first because the go-client wraps transport failures with %q, which
// destroys the error chain and would otherwise hide a cancelled or expired call.

// apiPrefix tags every message toGRPCError produces, so the autoscaler's logs show at a glance
// which errors came from the servers.com API.
const apiPrefix = "servers.com API"

// apiErrorf builds the gRPC status for an error caused by the servers.com API, prefixing the
// message so it reads consistently regardless of which case below produced it.
func apiErrorf(code codes.Code, format string, args ...any) error {
	return status.Errorf(code, apiPrefix+" "+format, args...)
}

func toGRPCError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	switch ctx.Err() {
	case context.DeadlineExceeded:
		return apiErrorf(codes.DeadlineExceeded, "call timed out: %v", err)
	case context.Canceled:
		return apiErrorf(codes.Canceled, "call cancelled: %v", err)
	}

	switch e := err.(type) {
	case *serverscom.ConflictError:
		return apiErrorf(codes.FailedPrecondition, "conflict: %s: %s", e.ErrorCode, e.Message)
	case *serverscom.NotFoundError:
		return apiErrorf(codes.NotFound, "not found: %s: %s", e.ErrorCode, e.Message)
	case *serverscom.BadRequestError:
		return apiErrorf(codes.InvalidArgument, "bad request: %s: %s", e.ErrorCode, e.Message)
	case *serverscom.UnprocessableEntityError:
		return apiErrorf(codes.InvalidArgument, "unprocessable entity: %s: %s: %v", e.ErrorCode, e.Message, e.Errors)
	case *serverscom.UnauthorizedError:
		return apiErrorf(codes.Unauthenticated, "unauthorized: %s: %s", e.ErrorCode, e.Message)
	case *serverscom.ForbiddenError:
		return apiErrorf(codes.PermissionDenied, "forbidden: %s: %s", e.ErrorCode, e.Message)
	case *serverscom.InternalServerError:
		// 5xx is Unavailable rather than Internal: the fault is on the API side and the
		// autoscaler may sensibly come back on its next loop. We never come back ourselves.
		return apiErrorf(codes.Unavailable, "internal error: %s: %s", e.ErrorCode, e.Message)
	case *serverscom.ParsingError:
		return apiErrorf(codes.Internal, "cannot parse response: %v", e)
	default:
		// Transport failures and any status code the go-client does not model land here.
		return apiErrorf(codes.Unavailable, "call failed: %v", err)
	}
}

// isNotFound reports whether err is an API 404.
func isNotFound(err error) bool {
	_, ok := err.(*serverscom.NotFoundError)
	return ok
}
