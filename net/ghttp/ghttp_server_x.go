package ghttp

import (
	"github.com/gogf/gf/v2/os/gsession"
)

// InitSession initialize session manager for test
func (s *Server) InitSession() {
	s.sessionManager = gsession.New(
		s.config.SessionMaxAge,
		s.config.SessionStorage,
	)
}
