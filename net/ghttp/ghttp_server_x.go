package ghttp

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/internal/intlog"
	"github.com/gogf/gf/v2/os/gsession"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/gogf/gf/v2/text/gstr"
)

// InitSession initialize session manager for test
func (s *Server) InitSession() {
	s.sessionManager = gsession.New(
		s.config.SessionMaxAge,
		s.config.SessionStorage,
	)
}

// HandlePreBindItems is called when server starts, which does really route registering to the server.
func (s *Server) HandlePreBindItems(ctx context.Context) {
	s.handlePreBindItems(ctx)
}

// ServerProcessInit initializes some process configurations, which can only be done once.
func ServerProcessInit() {
	serverProcessInit()
}

// CServeHTTP is the default handler for http request.
// It should not create new goroutine handling the request as
// it's called by am already created new goroutine from http.Server.
// @see ghttp_server_hanlder.ServerHTTP
// just switch handleErrorLog to cHandleErrorLog
func (s *Server) CServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Max body size limit.
	if s.config.ClientMaxBodySize > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, s.config.ClientMaxBodySize)
	}
	// In case of, eg:
	// Case 1:
	// 		GET /net/http
	// 		r.URL.Path    : /net/http
	// 		r.URL.RawPath : (empty string)
	// Case 2:
	// 		GET /net%2Fhttp
	// 		r.URL.Path    : /net/http
	// 		r.URL.RawPath : /net%2Fhttp
	if r.URL.RawPath != "" {
		r.URL.Path = r.URL.RawPath
	}
	// Rewrite feature checks.
	if len(s.config.Rewrites) > 0 {
		if rewrite, ok := s.config.Rewrites[r.URL.Path]; ok {
			r.URL.Path = rewrite
		}
	}

	// Create a new request object.
	request := newRequest(s, r, w)

	defer func() {
		request.LeaveTime = gtime.TimestampMilli()
		// error log handling.
		if request.error != nil {
			s.cHandleErrorLog(request.error, request)
		} else {
			if exception := recover(); exception != nil {
				request.Response.WriteStatus(http.StatusInternalServerError)
				if v, ok := exception.(error); ok {
					if code := gerror.Code(v); code != gcode.CodeNil {
						s.cHandleErrorLog(v, request)
					} else {
						s.cHandleErrorLog(gerror.WrapCodeSkip(gcode.CodeInternalError, 1, v, ""), request)
					}
				} else {
					s.cHandleErrorLog(gerror.NewCodeSkipf(gcode.CodeInternalError, 1, "%+v", exception), request)
				}
			}
		}
		// access log handling.
		s.handleAccessLog(request)
		// Close the session, which automatically update the TTL
		// of the session if it exists.
		if err := request.Session.Close(); err != nil {
			intlog.Errorf(request.Context(), `%+v`, err)
		}

		// Close the request and response body
		// to release the file descriptor in time.
		err := request.Request.Body.Close()
		if err != nil {
			intlog.Errorf(request.Context(), `%+v`, err)
		}
		if request.Request.Response != nil {
			err = request.Request.Response.Body.Close()
			if err != nil {
				intlog.Errorf(request.Context(), `%+v`, err)
			}
		}
	}()

	// ============================================================
	// Priority:
	// Static File > Dynamic Service > Static Directory
	// ============================================================

	// Search the static file with most high priority,
	// which also handle the index files feature.
	if s.config.FileServerEnabled {
		request.StaticFile = s.searchStaticFile(r.URL.Path)
		if request.StaticFile != nil {
			request.isFileRequest = true
		}
	}

	// Search the dynamic service handler.
	request.handlers, request.serveHandler, request.hasHookHandler, request.hasServeHandler = s.getHandlersWithCache(request)

	// Check the service type static or dynamic for current request.
	if request.StaticFile != nil && request.StaticFile.IsDir && request.hasServeHandler {
		request.isFileRequest = false
	}

	// HOOK - BeforeServe
	s.callHookHandler(HookBeforeServe, request)

	// Core serving handling.
	if !request.IsExited() {
		if request.isFileRequest {
			// Static file service.
			s.serveFile(request, request.StaticFile)
		} else {
			if len(request.handlers) > 0 {
				// Dynamic service.
				request.Middleware.Next()
			} else {
				if request.StaticFile != nil && request.StaticFile.IsDir {
					// Serve the directory.
					s.serveFile(request, request.StaticFile)
				} else {
					if len(request.Response.Header()) == 0 &&
						request.Response.Status == 0 &&
						request.Response.BufferLength() == 0 {
						request.Response.WriteHeader(http.StatusNotFound)
					}
				}
			}
		}
	}

	// HOOK - AfterServe
	if !request.IsExited() {
		s.callHookHandler(HookAfterServe, request)
	}

	// HOOK - BeforeOutput
	if !request.IsExited() {
		s.callHookHandler(HookBeforeOutput, request)
	}

	// HTTP status checking.
	if request.Response.Status == 0 {
		if request.StaticFile != nil || request.Middleware.served || request.Response.buffer.Len() > 0 {
			request.Response.WriteHeader(http.StatusOK)
		} else if err := request.GetError(); err != nil {
			if request.Response.BufferLength() == 0 {
				request.Response.Write(err.Error())
			}
			request.Response.WriteHeader(http.StatusInternalServerError)
		} else {
			request.Response.WriteHeader(http.StatusNotFound)
		}
	}
	// HTTP status handler.
	if request.Response.Status != http.StatusOK {
		statusFuncArray := s.getStatusHandler(request.Response.Status, request)
		for _, f := range statusFuncArray {
			// Call custom status handler.
			niceCallFunc(func() {
				f(request)
			})
			if request.IsExited() {
				break
			}
		}
	}

	// Automatically set the session id to cookie
	// if it creates a new session id in this request
	// and SessionCookieOutput is enabled.
	if s.config.SessionCookieOutput &&
		request.Session.IsDirty() &&
		request.Session.MustId() != request.GetSessionId() {
		request.Cookie.SetSessionId(request.Session.MustId())
	}
	// Output the cookie content to the client.
	request.Cookie.Flush()
	// Output the buffer content to the client.
	request.Response.Flush()
	// HOOK - AfterOutput
	if !request.IsExited() {
		s.callHookHandler(HookAfterOutput, request)
	}
}

// cHandleErrorLog handles the error logging for server.
// @see ghttp_server_log.handleErrorLog
// Why we custom this method? just want fire log.Error, not log.Print
func (s *Server) cHandleErrorLog(err error, r *Request) {
	// It does nothing if error logging is custom disabled.
	if !s.IsErrorLogEnabled() {
		return
	}
	var (
		code          = gerror.Code(err)
		scheme        = "http"
		codeDetail    = code.Detail()
		proto         = r.Header.Get("X-Forwarded-Proto")
		codeDetailStr string
	)
	if r.TLS != nil || gstr.Equal(proto, "https") {
		scheme = "https"
	}
	if codeDetail != nil {
		codeDetailStr = gstr.Replace(fmt.Sprintf(`%+v`, codeDetail), "\n", " ")
	}
	content := fmt.Sprintf(
		`%d "%s %s %s %s %s" %.3f, %s, "%s", "%s", %d, "%s", "%+v"`,
		r.Response.Status, r.Method, scheme, r.Host, r.URL.String(), r.Proto,
		float64(r.LeaveTime-r.EnterTime)/1000,
		r.GetClientIp(), r.Referer(), r.UserAgent(),
		code.Code(), code.Message(), codeDetailStr,
	)
	if s.config.ErrorStack {
		if stack := gerror.Stack(err); stack != "" {
			content += "\nStack:\n" + stack
		} else {
			content += ", " + err.Error()
		}
	} else {
		content += ", " + err.Error()
	}
	s.Logger().File(s.config.ErrorLogPattern).
		Stdout(s.config.LogStdout).
		Error(r.Context(), content)
}
