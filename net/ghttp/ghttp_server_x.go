package ghttp

import (
	"context"

	"github.com/gogf/gf/v2/os/gsession"
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
