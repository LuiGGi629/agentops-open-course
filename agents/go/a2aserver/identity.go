package a2aserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// The gateway-verified caller identity, carried from the HTTP boundary into the
// A2A call context.
//
// # Why this is two pieces and not one
//
// Python needed a pure-ASGI middleware and a ContextVar because its A2A request
// converter ran in a task the middleware could not reach any other way. Go has
// no such problem — a context value set on the request flows to every handler
// on the same request — but ADK Go reads the caller identity from exactly one
// place, a2asrv.CallContext.User.Name (server/adka2a/v2/metadata.go), and the
// A2A transport creates that CallContext itself, inside its own ServeHTTP,
// after any HTTP middleware has already run. So the value has to be handed
// across in two steps:
//
//  1. [Server.bindVerifiedIdentity] reads and validates the header at the HTTP
//     boundary — this is where a duplicate header can still be answered with a
//     400 — and stores the subject in the request context.
//  2. [identityInterceptor], installed with a2asrv.WithCallInterceptors, runs
//     inside the A2A handler once the CallContext exists and promotes the
//     subject onto it.
//
// # What depends on it
//
// Both of the things the Python comment names, and one more:
//
//   - the run's user id, which becomes the audit row's approved_by, because
//     adka2a's toInvocationMeta prefers CallContext.User.Name over the
//     synthetic "A2A_USER_<contextId>";
//   - the per-user long-term memory key, which is the same user id;
//   - task ownership, because a2asrv.NewTaskStoreAuthenticator reads the same
//     field, so ListTasks is scoped to the verified caller.
//
// # The trust assumption
//
// Only set AGENT_TRUSTED_IDENTITY_HEADER when a trusted gateway validates the
// JWT and *sets* this header itself, overwriting any client-supplied copy. A
// raw client could otherwise forge it. When the setting is empty the header is
// never read, so an unconfigured deployment cannot be tricked into trusting one.

// subjectKey types the request-context key. An unexported struct type cannot
// collide with a key from any other package.
type subjectKey struct{}

// duplicateIdentityHeader is the body returned when a request carries the
// trusted header twice. Two values mean at least one of them was not set by the
// gateway, and there is no safe way to pick between them.
const duplicateIdentityHeader = "duplicate trusted identity header"

// withVerifiedSubject returns a context carrying a gateway-verified subject.
func withVerifiedSubject(ctx context.Context, subject string) context.Context {
	return context.WithValue(ctx, subjectKey{}, subject)
}

// VerifiedSubject returns the gateway-verified caller identity bound to ctx, if
// any.
//
// It is exported because it is the honest way for anything downstream of the
// HTTP boundary to ask "who did the gateway say this is?" without re-reading a
// header it must not trust on its own.
func VerifiedSubject(ctx context.Context) (string, bool) {
	subject, ok := ctx.Value(subjectKey{}).(string)
	return subject, ok && subject != ""
}

// bindVerifiedIdentity reads the trusted identity header once per request.
//
// When no header name is configured the middleware is not installed at all, so
// there is no code path in which an unconfigured header is read.
func bindVerifiedIdentity(header string, logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Header.Values canonicalizes its argument, so it collects the header
		// under any capitalization — the case-insensitive match the ASGI
		// middleware did by lowercasing bytes.
		values := request.Header.Values(header)
		if len(values) > 1 {
			logger.WarnContext(request.Context(), "refused an A2A request",
				"reason", duplicateIdentityHeader, "header", header, "status", http.StatusBadRequest)
			writeJSON(writer, request, logger, http.StatusBadRequest,
				map[string]string{"error": duplicateIdentityHeader})
			return
		}
		if len(values) == 1 {
			if subject := strings.TrimSpace(values[0]); subject != "" {
				request = request.WithContext(withVerifiedSubject(request.Context(), subject))
			}
		}
		next.ServeHTTP(writer, request)
	})
}

// identityInterceptor promotes the verified subject onto the A2A call context.
//
// It embeds the SDK's passthrough so a new interceptor method added upstream
// does not silently become this type's responsibility.
type identityInterceptor struct {
	a2asrv.PassthroughCallInterceptor
}

// Before implements a2asrv.CallInterceptor.
//
// It runs for every protocol method, not only message sends, because task
// ownership is read on the listing path too: a caller who is authenticated well
// enough to create a task must be the same caller who can list it back.
func (identityInterceptor) Before(
	ctx context.Context, callCtx *a2asrv.CallContext, _ *a2asrv.Request,
) (context.Context, any, error) {
	subject, ok := VerifiedSubject(ctx)
	if !ok || callCtx == nil {
		// No verified subject means the unauthenticated synthetic identity
		// stands, which is the documented default for a deployment with no
		// gateway in front of it.
		return ctx, nil, nil
	}
	callCtx.User = a2asrv.NewAuthenticatedUser(subject, nil)
	return ctx, nil, nil
}

// writeJSON writes one small JSON body, logging a write failure rather than
// swallowing it.
func writeJSON(writer http.ResponseWriter, request *http.Request, logger *slog.Logger, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(body); err != nil {
		// The status line is already on the wire, so the client sees a truncated
		// body and there is nothing left to correct. Logging is the only honest
		// response; silence would hide a probe that answers with nothing.
		logger.ErrorContext(request.Context(), "writing an A2A response body", "error", err)
	}
}
