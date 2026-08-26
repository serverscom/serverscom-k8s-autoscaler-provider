package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestToGRPCError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode codes.Code
		contains string
	}{
		{
			name:     "conflict, group at its maximum",
			err:      &serverscom.ConflictError{StatusCode: 409, ErrorCode: "MAXIMUM_NODES_REACHED", Message: "This group has reached its maximum size"},
			wantCode: codes.FailedPrecondition,
			contains: "MAXIMUM_NODES_REACHED",
		},
		{
			name:     "conflict, target below the registered nodes",
			err:      &serverscom.ConflictError{StatusCode: 409, ErrorCode: "NODES_ALREADY_REGISTERED", Message: "The target size cannot drop below the nodes the group already has"},
			wantCode: codes.FailedPrecondition,
			contains: "NODES_ALREADY_REGISTERED",
		},
		{
			name:     "conflict, node has not joined yet",
			err:      &serverscom.ConflictError{StatusCode: 409, ErrorCode: "K8S_NODES_NOT_READY", Message: "A node can only be deleted once it has joined the cluster"},
			wantCode: codes.FailedPrecondition,
			contains: "K8S_NODES_NOT_READY",
		},
		{
			name:     "not found",
			err:      &serverscom.NotFoundError{StatusCode: 404, ErrorCode: "NOT_FOUND", Message: "Not found"},
			wantCode: codes.NotFound,
			contains: "NOT_FOUND",
		},
		{
			name:     "bad request",
			err:      &serverscom.BadRequestError{StatusCode: 400, ErrorCode: "BAD_REQUEST", Message: "nope"},
			wantCode: codes.InvalidArgument,
			contains: "BAD_REQUEST",
		},
		{
			name:     "unprocessable entity",
			err:      &serverscom.UnprocessableEntityError{StatusCode: 422, ErrorCode: "VALIDATION_ERROR", Message: "nope", Errors: map[string]string{"delta": "must be positive"}},
			wantCode: codes.InvalidArgument,
			contains: "must be positive",
		},
		{
			name:     "unauthorized",
			err:      &serverscom.UnauthorizedError{StatusCode: 401, ErrorCode: "UNAUTHORIZED", Message: "bad token"},
			wantCode: codes.Unauthenticated,
			contains: "UNAUTHORIZED",
		},
		{
			name:     "forbidden",
			err:      &serverscom.ForbiddenError{StatusCode: 403, ErrorCode: "FORBIDDEN", Message: "no"},
			wantCode: codes.PermissionDenied,
			contains: "FORBIDDEN",
		},
		{
			name:     "internal server error",
			err:      &serverscom.InternalServerError{StatusCode: 500, ErrorCode: "INTERNAL", Message: "boom"},
			wantCode: codes.Unavailable,
			contains: "INTERNAL",
		},
		{
			name:     "unparsable body",
			err:      &serverscom.ParsingError{StatusCode: 200, Body: "<html>", ParsingError: errors.New("invalid character")},
			wantCode: codes.Internal,
		},
		{
			name: "service unavailable, not modelled by the client",
			// the go-client returns a plain error for status codes it does not model, such as
			// the 503 the API answers with while a region is in maintenance
			err:      errors.New("Unexpected response code: 503, with body: CLOUD_REGION_MAINTENANCE"),
			wantCode: codes.Unavailable,
			contains: "CLOUD_REGION_MAINTENANCE",
		},
		{
			name:     "broken connection",
			err:      errors.New(`Client request error: "connection refused"`),
			wantCode: codes.Unavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			err := toGRPCError(context.Background(), tc.err)

			g.Expect(status.Code(err)).To(Equal(tc.wantCode))
			if tc.contains != "" {
				g.Expect(err.Error()).To(ContainSubstring(tc.contains))
			}
		})
	}
}

func TestToGRPCErrorNil(t *testing.T) {
	g := NewGomegaWithT(t)

	g.Expect(toGRPCError(context.Background(), nil)).To(BeNil())
}

// A timed out call is reported as such rather than as a generic failure, so that the autoscaler
// can tell "the API did not answer" from "the API said no".
func TestToGRPCErrorTimeout(t *testing.T) {
	g := NewGomegaWithT(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	<-ctx.Done()

	err := toGRPCError(ctx, errors.New(`Client request error: "context deadline exceeded"`))

	g.Expect(status.Code(err)).To(Equal(codes.DeadlineExceeded))
}

func TestToGRPCErrorCancelled(t *testing.T) {
	g := NewGomegaWithT(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := toGRPCError(ctx, errors.New(`Client request error: "context canceled"`))

	g.Expect(status.Code(err)).To(Equal(codes.Canceled))
}
